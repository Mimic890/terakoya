package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/search"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// Search: any text outside a dialog is a query, as is /search <query>;
// /search alone asks for one. The results replace the menu, a page at a
// time; a result opens like any note, and its «Назад» returns to them.

// stateSearch waits for the query after /search alone.
const stateSearch = "search"

const (
	cbSearchPage    = "sp:" // sp:<page> of the chat's last search
	cbSearchSuggest = "ss:" // ss:<n>, a query offered for one that found nothing
	cbSearchOpen    = "sa:" // sa:<note id>, a result
	cbBackSearch    = "bs:" // bs:<note id>, «Назад» of a note opened from the results
	// sr:<subject id>, «Сообщить, что не хватает материала»; sr: alone
	// asks which subject, when there are several.
	cbSearchReport = "sr:"
)

const (
	searchPerPage  = 8
	snippetWidth   = 120
	maxSuggestions = 3
	// maxQuery bounds a query, in letters.
	maxQuery = 200
	// maxSearches is how many chats' last searches are kept.
	maxSearches = 10000
)

// lastSearch is a chat's last search: its query, the page shown and the
// queries offered when it found nothing. It is kept in memory only; after
// a restart, its buttons ask for the query again.
type lastSearch struct {
	query       string
	page        int
	suggestions []string
}

func (b *Bot) searchIndex() *search.Index {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.index
}

func (b *Bot) rememberSearch(chatID int64, s lastSearch) {
	b.searchMu.Lock()
	defer b.searchMu.Unlock()
	if b.searches == nil || len(b.searches) >= maxSearches {
		b.searches = map[int64]lastSearch{}
	}
	b.searches[chatID] = s
}

func (b *Bot) lastSearchOf(chatID int64) (lastSearch, bool) {
	b.searchMu.Lock()
	defer b.searchMu.Unlock()
	s, ok := b.searches[chatID]
	return s, ok
}

// askSearch answers /search alone: the next message is the query.
func (b *Bot) askSearch(ctx context.Context, m *models.Message) {
	unlock := b.userLocks.lock(m.From.ID)
	defer unlock()
	d := &dialog{chatID: m.Chat.ID, ses: store.Session{TgID: m.From.ID, State: stateSearch}}
	b.prompt(ctx, d, b.t.SearchPrompt, keyboard(cancelRow(b.t.Cancel)))
}

// onSearchMessage takes the query asked for by /search. The question
// and the query go once the results are shown.
func (b *Bot) onSearchMessage(ctx context.Context, d *dialog, m *models.Message) {
	text := strings.TrimSpace(m.Text)
	if text == "" {
		b.prompt(ctx, d, b.t.SearchPrompt, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	b.endDialog(ctx, d)
	b.runSearch(ctx, d.chatID, text)
	b.dropMessages(ctx, d.chatID, d.p.Msgs)
}

// runSearch searches, records the query for /admin and shows the first
// page of the results.
func (b *Bot) runSearch(ctx context.Context, chatID int64, query string) {
	start := time.Now()
	query = oneLine(query)
	if utf8.RuneCountInString(query) > maxQuery {
		query = string([]rune(query)[:maxQuery])
	}
	ix := b.searchIndex()
	r := ix.Search(query)
	s := lastSearch{query: query}
	if len(r.Hits) == 0 {
		s.suggestions = ix.Suggest(query, maxSuggestions)
	}
	b.rememberSearch(chatID, s)
	if q := search.Normalize(query); q != "" {
		if err := b.store.LogSearch(ctx, q, len(r.Hits)); err != nil {
			b.log.Error("log search", "err", err)
		}
	}
	b.showResults(ctx, chatID, s, r)
	b.log.Info("search", "results", len(r.Hits), "ms", time.Since(start).Milliseconds())
}

// showSearch shows a chat's last search again, at its page.
func (b *Bot) showSearch(ctx context.Context, chatID int64, s lastSearch) {
	b.showResults(ctx, chatID, s, b.searchIndex().Search(s.query))
}

// showResults shows a page of results: each a link with its kind and
// title, where it is, and the text around the words found. A search that
// found nothing offers other queries and a report of the missing
// material.
func (b *Bot) showResults(ctx context.Context, chatID int64, s lastSearch, r search.Result) {
	title := fmt.Sprintf(b.t.SearchTitle, s.query)
	var sb strings.Builder
	sb.WriteString("# " + escapeMarkdown(title) + "\n\n")
	var rows [][]models.InlineKeyboardButton
	if len(r.Hits) == 0 {
		sb.WriteString(escapeMarkdown(b.t.SearchNothing) + "\n")
		if len(s.suggestions) > 0 {
			sb.WriteString("\n" + escapeMarkdown(b.t.SearchDidYouMean) + "\n")
			for i, q := range s.suggestions {
				rows = append(rows, row(button(q, cbSearchSuggest+strconv.Itoa(i))))
			}
		}
		if b.host != nil {
			data := cbSearchReport
			if subjects := b.catalog().Subjects; len(subjects) == 1 {
				data += subjects[0].ID
			}
			rows = append(rows, row(button(b.t.SearchReport, data)))
		}
		rows = append(rows, row(button(b.t.Close, cbClose)))
		b.showRich(ctx, chatID, title, sb.String(), keyboard(rows...))
		return
	}
	pages := (len(r.Hits) + searchPerPage - 1) / searchPerPage
	page := min(max(s.page, 0), pages-1)
	found := fmt.Sprintf(b.t.SearchFound, len(r.Hits))
	if pages > 1 {
		found += " · " + fmt.Sprintf(b.t.SearchPage, page+1, pages)
	}
	sb.WriteString(escapeMarkdown(found) + "\n")
	for _, h := range r.Hits[page*searchPerPage : min(len(r.Hits), (page+1)*searchPerPage)] {
		sb.WriteString("\n" + b.resultMarkdown(h, r) + "\n")
	}
	var nav []models.InlineKeyboardButton
	if page > 0 {
		nav = append(nav, button(b.t.SearchPrev, cbSearchPage+strconv.Itoa(page-1)))
	}
	if page < pages-1 {
		nav = append(nav, button(b.t.SearchNext, cbSearchPage+strconv.Itoa(page+1)))
	}
	if nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, row(button(b.t.Close, cbClose)))
	b.showRich(ctx, chatID, title, sb.String(), keyboard(rows...))
}

// resultMarkdown is one result: «Теорема · title», which opens it,
// «Геометрия · 10 класс · §5 topic» and the snippet, a line each.
func (b *Bot) resultMarkdown(h search.Hit, r search.Result) string {
	n := h.Note
	kind := n.KindLabel()
	if n.IsMain() || kind == "" {
		kind = b.t.SearchMain
	}
	lines := []string{
		menuLink(kind+" · "+n.Title, cbSearchOpen+n.ID),
		escapeLineStart(escapeMarkdown(strings.Join([]string{n.Subject().Name, n.Grade().Title(), topicLabel(n.Topic)}, " · "))),
	}
	if snip := snippetMarkdown(r.Snippet(h, snippetWidth)); snip != "" {
		lines = append(lines, snip)
	}
	// Two trailing spaces break the line within a paragraph.
	return strings.Join(lines, "  \n")
}

// snippetMarkdown writes a snippet with the words found in bold.
func snippetMarkdown(pieces []search.Piece) string {
	var sb strings.Builder
	for _, p := range pieces {
		if p.Bold {
			sb.WriteString("**" + escapeMarkdown(p.Text) + "**")
		} else {
			sb.WriteString(escapeMarkdown(p.Text))
		}
	}
	return escapeLineStart(sb.String())
}

// escapeLineStart keeps a line from starting a list: «- », «+ » or «1. ».
func escapeLineStart(s string) string {
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return `\` + s
	}
	digits := len(s) - len(strings.TrimLeft(s, "0123456789"))
	if digits > 0 && digits < len(s) && (s[digits] == '.' || s[digits] == ')') {
		return s[:digits] + `\` + s[digits:]
	}
	return s
}

// onSearchCallback handles the buttons of the results and of the notes
// opened from them. It reports false for other buttons.
func (b *Bot) onSearchCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) bool {
	data, chatID := q.Data, msg.Chat.ID
	cat := b.catalog()
	switch {
	case strings.HasPrefix(data, cbSearchOpen):
		n := cat.Note(strings.TrimPrefix(data, cbSearchOpen))
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return true
		}
		b.answer(ctx, q, "", false)
		if b.sendArticle(ctx, chatID, n, cbBackSearch) {
			b.retire(ctx, msg)
		}

	case strings.HasPrefix(data, cbBackSearch):
		b.answer(ctx, q, "", false)
		if s, ok := b.lastSearchOf(chatID); ok {
			b.retire(ctx, msg)
			b.showSearch(ctx, chatID, s)
			return true
		}
		// The results are gone with a restart: back as from the catalog.
		b.back(ctx, q, msg, cat.Note(strings.TrimPrefix(data, cbBackSearch)))

	case strings.HasPrefix(data, cbSearchPage):
		s, ok := b.lastSearchOf(chatID)
		page, err := strconv.Atoi(strings.TrimPrefix(data, cbSearchPage))
		if !ok || err != nil {
			b.answer(ctx, q, b.t.SearchExpired, true)
			return true
		}
		b.answer(ctx, q, "", false)
		s.page = page
		b.rememberSearch(chatID, s)
		b.showSearch(ctx, chatID, s)

	case strings.HasPrefix(data, cbSearchSuggest):
		s, ok := b.lastSearchOf(chatID)
		i, err := strconv.Atoi(strings.TrimPrefix(data, cbSearchSuggest))
		if !ok || err != nil || i < 0 || i >= len(s.suggestions) {
			b.answer(ctx, q, b.t.SearchExpired, true)
			return true
		}
		b.answer(ctx, q, "", false)
		b.runSearch(ctx, chatID, s.suggestions[i])

	case strings.HasPrefix(data, cbSearchReport):
		s, ok := b.lastSearchOf(chatID)
		if !ok {
			b.answer(ctx, q, b.t.SearchExpired, true)
			return true
		}
		id := strings.TrimPrefix(data, cbSearchReport)
		if id == "" {
			b.answer(ctx, q, "", false)
			var rows [][]models.InlineKeyboardButton
			for _, sub := range cat.Subjects {
				rows = append(rows, row(button(sub.Name, cbSearchReport+sub.ID)))
			}
			rows = append(rows, row(button(b.t.Back, cbSearchPage+strconv.Itoa(s.page))))
			b.replaceMenu(ctx, chatID, b.t.SearchReportChoose, keyboard(rows...))
			return true
		}
		sub := cat.Subject(id)
		if sub == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return true
		}
		b.startSearchReport(ctx, q, chatID, sub, s.query)

	default:
		return false
	}
	return true
}

// startSearchReport reports material missing in subject s: the report
// starts with the query that found nothing, and can be sent as it is.
func (b *Bot) startSearchReport(ctx context.Context, q *models.CallbackQuery, chatID int64, s *content.Subject, query string) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	base, ok := b.canReport(ctx, q)
	if !ok {
		return
	}
	b.answer(ctx, q, "", false)
	d := &dialog{chatID: chatID,
		ses: store.Session{TgID: q.From.ID, State: stateReportConfirm, BaseSHA: base},
		p: sessionPayload{SubjectID: s.ID, ShortID: suggest.ShortID(), OriginMsg: q.Message.Message.ID,
			Text: fmt.Sprintf(b.t.SearchReportText, query), SearchQuery: query}}
	b.prompt(ctx, d, fmt.Sprintf(b.t.SearchReportPrompt, s.Name, query), b.sendKeyboard(b.t.Send))
}
