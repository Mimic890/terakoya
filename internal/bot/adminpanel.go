package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/stats"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// /admin is one menu message for admins: a summary, and screens for
// users, statistics, the server, syncing the vault and broadcasts.

const (
	cbAdmin        = "ad:"
	cbAdminMain    = "ad:m"
	cbAdminUsers   = "ad:u:" // ad:u:<page>
	cbAdminClear   = "ad:ux"
	cbAdminFind    = "ad:uf"
	cbAdminCard    = "ad:c:"  // ad:c:<user id>
	cbAdminReset   = "ad:n:"  // ad:n:<user id>, asks
	cbAdminResetOK = "ad:ny:" // ad:ny:<user id>
	cbAdminStats   = "ad:s"
	cbAdminServer  = "ad:v"
	cbAdminSync    = "ad:y"
	cbAdminBcast   = "ad:b"
	cbAdminNoop    = "ad:-"

	stateAdminSearch = "admin_search" // waiting for a search query

	// Heavy work runs in the background with these limits.
	serverTimeout = 30 * time.Second
	syncTimeout   = 2 * time.Minute
)

// adminView is where an admin is in the user list.
type adminView struct {
	query string
	page  int
}

func (b *Bot) view(adminID int64) adminView {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	return b.views[adminID]
}

func (b *Bot) setView(adminID int64, v adminView) {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	b.views[adminID] = v
}

// showAdmin handles /admin.
func (b *Bot) showAdmin(ctx context.Context, chatID int64) {
	text, kb := b.adminMainView(ctx)
	b.replaceMenu(ctx, chatID, text, kb)
}

func (b *Bot) adminMainView(ctx context.Context) (string, *models.InlineKeyboardMarkup) {
	st, err := b.AdminStatus(ctx)
	if err != nil {
		b.log.Error("admin status", "err", err)
	}
	text := b.t.AdminTitle + "\n\n" + fmt.Sprintf(b.t.AdminSummary, st.Users, st.Active24h, st.OpenReports,
		short(st.Commit), b.ago(st.CommitTime), b.duration(time.Duration(st.Uptime)))
	return text, keyboard(
		row(button(b.t.AdminUsers, cbAdminUsers+"0"), button(b.t.AdminStats, cbAdminStats)),
		row(button(b.t.AdminServer, cbAdminServer), button(b.t.AdminSync, cbAdminSync)),
		row(button(b.t.AdminBroadcast, cbAdminBcast), button(b.t.Close, cbClose)),
	)
}

// short is a commit's short form.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "—"
	}
	return sha
}

// duration formats a span: «3 д 4 ч», «2 ч 5 мин», «12 мин», «40 с».
func (b *Bot) duration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf(b.t.AdminDays, int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf(b.t.AdminHours, int(d/time.Hour), int(d%time.Hour/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf(b.t.AdminMinutes, int(d/time.Minute))
	}
	return fmt.Sprintf(b.t.AdminSeconds, int(d/time.Second))
}

// ago is «2 ч 5 мин назад», or «нет данных» for the zero time.
func (b *Bot) ago(t time.Time) string {
	if t.IsZero() {
		return b.t.AdminNoData
	}
	return fmt.Sprintf(b.t.AdminAgo, b.duration(b.clock().Sub(t)))
}

// moment is «19 октября, 12:30».
func (b *Bot) moment(t time.Time) string {
	if t.IsZero() {
		return b.t.AdminNoData
	}
	return b.date(t) + ", " + t.Format("15:04")
}

// onAdminCallback handles the buttons of /admin. Others get nothing.
func (b *Bot) onAdminCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	if !b.cfg.IsAdmin(q.From.ID) {
		b.answer(ctx, q, "", false)
		return
	}
	data, chatID, admin := q.Data, msg.Chat.ID, q.From.ID
	id := func(prefix string) int64 {
		n, _ := strconv.ParseInt(strings.TrimPrefix(data, prefix), 10, 64)
		return n
	}
	switch {
	case data == cbAdminMain:
		b.answer(ctx, q, "", false)
		text, kb := b.adminMainView(ctx)
		b.editMenu(ctx, chatID, msg.ID, text, kb)
	case data == cbAdminClear:
		b.answer(ctx, q, "", false)
		b.setView(admin, adminView{})
		b.showUsers(ctx, chatID, msg.ID, admin)
	case strings.HasPrefix(data, cbAdminUsers):
		b.answer(ctx, q, "", false)
		v := b.view(admin)
		v.page = int(id(cbAdminUsers))
		b.setView(admin, v)
		b.showUsers(ctx, chatID, msg.ID, admin)
	case data == cbAdminFind:
		b.answer(ctx, q, "", false)
		b.startAdminSearch(ctx, chatID, admin, msg.ID)
	case strings.HasPrefix(data, cbAdminCard):
		b.answer(ctx, q, "", false)
		b.showCard(ctx, chatID, msg.ID, admin, id(cbAdminCard))
	case strings.HasPrefix(data, cbAdminResetOK):
		_, _, err := b.ResetNickname(ctx, strconv.FormatInt(id(cbAdminResetOK), 10), admin)
		toast := b.t.AdminResetDone
		if err != nil && err != ErrNoNickname {
			b.log.Error("reset nickname", "err", err)
			toast = b.t.SubmitFailed
		}
		b.answer(ctx, q, toast, false)
		b.showCard(ctx, chatID, msg.ID, admin, id(cbAdminResetOK))
	case strings.HasPrefix(data, cbAdminReset):
		b.answer(ctx, q, "", false)
		u, err := b.AdminUser(ctx, strconv.FormatInt(id(cbAdminReset), 10))
		if err != nil || u.Nickname == "" {
			b.showCard(ctx, chatID, msg.ID, admin, id(cbAdminReset))
			return
		}
		ref, who := strconv.FormatInt(u.ID, 10), u.FirstName
		if u.Username != "" {
			who = "@" + u.Username
		}
		b.editMenu(ctx, chatID, msg.ID, fmt.Sprintf(b.t.AdminResetAsk, u.Nickname, who), keyboard(
			row(button(b.t.AdminResetYes, cbAdminResetOK+ref), button(b.t.No, cbAdminCard+ref))))
	case data == cbAdminStats:
		b.answer(ctx, q, "", false)
		b.showAdminStats(ctx, chatID)
	case data == cbAdminServer:
		b.answer(ctx, q, "", false)
		b.editMenu(ctx, chatID, msg.ID, b.t.AdminComputing, keyboard())
		b.later(ctx, func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, serverTimeout)
			defer cancel()
			text, kb := b.adminServerView(ctx)
			b.editMenu(ctx, chatID, msg.ID, text, kb)
		})
	case data == cbAdminSync:
		b.answer(ctx, q, "", false)
		b.editMenu(ctx, chatID, msg.ID, b.t.AdminSyncing, keyboard())
		b.later(ctx, func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, syncTimeout)
			defer cancel()
			b.editMenu(ctx, chatID, msg.ID, b.syncText(b.SyncNow(ctx)), keyboard(row(button(b.t.Back, cbAdminMain))))
		})
	case data == cbAdminBcast:
		b.answer(ctx, q, "", false)
		b.startBroadcast(ctx, chatID, admin)
	default:
		b.answer(ctx, q, "", false)
	}
}

// showUsers shows the admin's page of the user list in msgID.
func (b *Bot) showUsers(ctx context.Context, chatID int64, msgID int, admin int64) {
	v := b.view(admin)
	list, err := b.AdminUsers(ctx, v.query, v.page)
	if err != nil {
		b.log.Error("admin users", "err", err)
	}
	if list.Page != v.page {
		v.page = list.Page
		b.setView(admin, v)
	}
	text, kb := b.usersView(list)
	if msgID != 0 {
		b.editMenu(ctx, chatID, msgID, text, kb)
	} else {
		b.replaceMenu(ctx, chatID, text, kb)
	}
}

// usersView renders a page of the user list.
func (b *Bot) usersView(l AdminUsers) (string, *models.InlineKeyboardMarkup) {
	c := l.Counts
	text := fmt.Sprintf(b.t.AdminUsersText, c.All, c.NewToday, c.New7d, c.Active24h, c.Active7d, c.Blocked)
	if l.Query != "" {
		text += "\n\n" + fmt.Sprintf(b.t.AdminFound, l.Query, l.Found)
	}
	if l.Found == 0 {
		text += "\n\n" + b.t.AdminNobody
	}
	var rows [][]models.InlineKeyboardButton
	for _, u := range l.Users {
		label := u.Name
		if u.Username != "" && !strings.HasPrefix(u.Name, "@") {
			label += " · @" + u.Username
		}
		rows = append(rows, row(button(label, cbAdminCard+strconv.FormatInt(u.ID, 10))))
	}
	if l.Pages > 1 {
		prev, next := max(l.Page-1, 0), min(l.Page+1, l.Pages-1)
		rows = append(rows, row(button("‹", cbAdminUsers+strconv.Itoa(prev)),
			button(fmt.Sprintf("%d/%d", l.Page+1, l.Pages), cbAdminNoop), button("›", cbAdminUsers+strconv.Itoa(next))))
	}
	search := row(button(b.t.AdminSearch, cbAdminFind))
	if l.Query != "" {
		search = append(search, button(b.t.AdminSearchClear, cbAdminClear))
	}
	rows = append(rows, search, row(button(b.t.Back, cbAdminMain)))
	return text, keyboard(rows...)
}

// showCard shows a user's card in msgID.
func (b *Bot) showCard(ctx context.Context, chatID int64, msgID int, admin, userID int64) {
	u, err := b.AdminUser(ctx, strconv.FormatInt(userID, 10))
	if err != nil {
		b.showUsers(ctx, chatID, msgID, admin)
		return
	}
	text, kb := b.cardView(u, b.view(admin).page)
	b.editMenu(ctx, chatID, msgID, text, kb)
}

// cardView renders a user's card; page is the list page to go back to.
func (b *Bot) cardView(u AdminUser, page int) (string, *models.InlineKeyboardMarkup) {
	ids := fmt.Sprintf(b.t.AdminCardID, u.ID)
	if u.Username != "" {
		ids = fmt.Sprintf(b.t.AdminCardUserID, u.Username, u.ID)
	}
	yesNo := func(v bool) string {
		if v {
			return b.t.SettingsYes
		}
		return b.t.SettingsNo
	}
	nick, top := u.Nickname, b.t.AdminShown
	if nick == "" {
		nick = b.t.AdminNone
	}
	if u.HideTop {
		top = b.t.AdminHidden
	}
	text := fmt.Sprintf(b.t.AdminCard, u.Name, ids, nick, u.Rank, u.Accepted, u.Open, u.Rejected,
		b.moment(u.FirstSeen), b.moment(u.LastSeen), top, yesNo(u.Blocked))
	var rows [][]models.InlineKeyboardButton
	if u.Nickname != "" {
		rows = append(rows, row(button(b.t.AdminResetNick, cbAdminReset+strconv.FormatInt(u.ID, 10))))
	}
	rows = append(rows, row(button(b.t.Back, cbAdminUsers+strconv.Itoa(page))))
	return text, keyboard(rows...)
}

// startAdminSearch asks what to look for; the list stays above.
func (b *Bot) startAdminSearch(ctx context.Context, chatID, admin int64, menu int) {
	unlock := b.userLocks.lock(admin)
	defer unlock()
	if d, _ := b.loadDialog(ctx, admin, chatID); d != nil {
		b.dropButtons(ctx, d)
	}
	d := &dialog{chatID: chatID, ses: store.Session{TgID: admin, State: stateAdminSearch},
		p: sessionPayload{OriginMsg: menu}}
	b.prompt(ctx, d, b.t.AdminSearchAsk, keyboard(cancelRow(b.t.Cancel)))
}

// onAdminSearch takes the query: the dialog goes and the list shows what
// was found.
func (b *Bot) onAdminSearch(ctx context.Context, d *dialog, m *models.Message) {
	q := strings.TrimSpace(m.Text)
	if q == "" {
		b.prompt(ctx, d, b.t.AdminSearchAsk, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	b.endDialog(ctx, d)
	b.dropMessages(ctx, d.chatID, d.p.Msgs)
	b.setView(m.From.ID, adminView{query: q})
	b.showUsers(ctx, d.chatID, d.p.OriginMsg, m.From.ID)
}

// adminServerView measures and renders the «Сервер» screen.
func (b *Bot) adminServerView(ctx context.Context) (string, *models.InlineKeyboardMarkup) {
	kb := keyboard(row(button(b.t.AdminRefresh, cbAdminServer), button(b.t.Back, cbAdminMain)))
	s, err := b.AdminServer(ctx)
	if err != nil {
		b.log.Error("admin server", "err", err)
		return fmt.Sprintf(b.t.AdminServerFailed, err), kb
	}
	return b.serverText(s), kb
}

func (b *Bot) serverText(s AdminServer) string {
	last := b.t.AdminSyncNever
	if r := s.LastSync; r != nil {
		outcome := b.t.AdminSyncSame
		switch {
		case r.Err != "":
			outcome = fmt.Sprintf(b.t.AdminSyncFailed, r.Err)
		case r.Changed():
			outcome = short(r.OldCommit) + " → " + short(r.NewCommit)
		}
		last = fmt.Sprintf(b.t.AdminLastSync, b.moment(r.At), outcome)
	}
	B := stats.Bytes
	return fmt.Sprintf(b.t.AdminServerText,
		B(s.RSS), B(s.Heap), s.Goroutines,
		b.duration(time.Duration(s.Uptime)), short(s.Version),
		B(s.NetRx), B(s.NetTx),
		s.APIToday.Calls, B(s.APIToday.Sent), B(s.APIToday.Received),
		s.APITotal.Calls, B(s.APITotal.Sent), B(s.APITotal.Received),
		B(s.Vault.Total), B(s.Vault.Markdown), B(s.Vault.Images), B(s.Vault.Git),
		B(s.DataDir), B(s.DB), s.MediaRows,
		short(s.Commit), b.ago(s.CommitTime), s.Notes, last) + "\n" + b.backupLine(s.Backup)
}

// backupLine is the backups on the «Сервер» screen: the mode, the last
// one and the next run.
func (b *Bot) backupLine(r BackupResult) string {
	last := b.t.AdminBackupNone
	switch {
	case r.Err != "":
		last = fmt.Sprintf(b.t.AdminBackupFailed, b.moment(r.At), r.Err)
	case !r.At.IsZero():
		last = b.moment(r.At) + ", " + stats.Bytes(r.Size)
	}
	next := b.t.AdminBackupUnscheduled
	if !r.Next.IsZero() {
		next = b.moment(r.Next)
	}
	return fmt.Sprintf(b.t.AdminBackup, b.backupMode(r.Mode), last, next)
}

// backupMode names a backup mode for /admin.
func (b *Bot) backupMode(mode string) string {
	switch mode {
	case "archive":
		return b.t.AdminBackupArchive
	case "off":
		return b.t.AdminBackupOff
	}
	return b.t.AdminBackupFolder
}

// syncText renders the outcome of «Обновить vault».
func (b *Bot) syncText(r SyncResult) string {
	switch {
	case r.Err != "":
		return fmt.Sprintf(b.t.AdminSyncFailed, r.Err)
	case !r.Changed():
		return fmt.Sprintf(b.t.AdminSyncNone, short(r.NewCommit))
	}
	text := fmt.Sprintf(b.t.AdminSyncDone, short(r.OldCommit), short(r.NewCommit), r.NotesBefore, r.NotesAfter,
		r.Errors, r.Warnings)
	for _, i := range r.Issues {
		text += "\n• " + i
	}
	return text
}

// showAdminStats replaces the menu with the statistics, a rich message
// whose note titles open the notes.
func (b *Bot) showAdminStats(ctx context.Context, chatID int64) {
	s, err := b.AdminStats(ctx)
	if err != nil {
		b.log.Error("admin stats", "err", err)
	}
	b.showRich(ctx, chatID, b.t.AdminStatsTitle, b.statsMarkdown(s),
		keyboard(row(button(b.t.Back, cbAdminMain), button(b.t.Close, cbClose))))
}

func (b *Bot) statsMarkdown(s AdminStats) string {
	var sb strings.Builder
	sb.WriteString("# " + b.t.AdminStatsTitle + "\n\n## " + b.t.AdminReportsTitle + "\n\n")
	line := func(period string, c store.SuggestionCounts) string {
		return escapeMarkdown(fmt.Sprintf(b.t.AdminReportsLine, period, c.Open, c.Accepted, c.Rejected))
	}
	sb.WriteString(line(b.t.AdminAllTime, s.Reports) + "  \n" + line(b.t.AdminWeek, s.ReportsWeek) + "\n")
	for _, top := range []struct {
		title string
		notes []NoteStat
	}{{b.t.AdminTopWeek, s.TopWeek}, {b.t.AdminTopAll, s.TopAll}} {
		sb.WriteString("\n## " + top.title + "\n\n")
		if len(top.notes) == 0 {
			sb.WriteString(b.t.AdminNoOpens + "\n")
			continue
		}
		var lines []string
		for i, n := range top.notes {
			title := escapeMarkdown(fmt.Sprintf(b.t.AdminNoteGone, n.ID))
			if n.Path != "" {
				// A link to the note, which the renderer makes a button.
				title = "[" + strings.NewReplacer("[", "", "]", "").Replace(n.Title) + "](" + suggest.LinkTarget(n.Path) + ")"
			}
			lines = append(lines, fmt.Sprintf("%d\\. %s — %d", i+1, title, n.Opens))
		}
		sb.WriteString(strings.Join(lines, "  \n") + "\n")
	}
	sb.WriteString("\n## " + b.t.AdminBroadcast + "\n\n" + escapeMarkdown(fmt.Sprintf(b.t.AdminBroadcasts, s.Broadcasts)))
	if bc := s.LastBroadcast; bc != nil {
		sb.WriteString("  \n" + escapeMarkdown(fmt.Sprintf(b.t.AdminLastBcast, b.moment(bc.Created), bc.Recipients,
			bc.Sent, bc.Blocked, bc.Failed)))
	}
	sb.WriteString("\n")
	return sb.String()
}
