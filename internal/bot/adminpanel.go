package bot

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/stats"
	"github.com/Mimic890/terakoya/internal/store"
)

// /admin is a rich message for admins: the server, the vault, users,
// reports, documents and searches, in tables. Under it are «Пользователи»,
// a table of users page by page, and «Обновить vault». Rich messages
// cannot be edited, so every screen is sent anew and the old one goes.
// /broadcast is a separate admin command, not part of it. No emoji here.

const (
	cbAdmin      = "ad:"
	cbAdminMain  = "ad:m"
	cbAdminUsers = "ad:u:" // ad:u:<page>
	cbAdminSync  = "ad:y"

	// Heavy work runs in the background with these limits.
	panelTimeout = 30 * time.Second
	syncTimeout  = 2 * time.Minute
	// syncShownFor is how long the outcome of a clean sync stays.
	syncShownFor = time.Second
)

// showAdmin handles /admin and «Обновить»: the panel is measured in the
// background, as CPU use takes a moment to measure.
func (b *Bot) showAdmin(ctx context.Context, chatID int64) {
	b.later(ctx, func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, panelTimeout)
		defer cancel()
		b.sendPanel(ctx, chatID)
	})
}

func (b *Bot) sendPanel(ctx context.Context, chatID int64) {
	b.showRich(ctx, chatID, b.t.AdminTitle, b.panelMarkdown(ctx), keyboard(
		row(button(b.t.AdminUsers, cbAdminUsers+"0"), button(b.t.AdminSync, cbAdminSync)),
		row(button(b.t.AdminRefresh, cbAdminMain)),
	))
}

// onAdminCallback handles the buttons of /admin. Others get nothing.
func (b *Bot) onAdminCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	b.answer(ctx, q, "", false)
	if !b.cfg.IsAdmin(q.From.ID) {
		return
	}
	chatID := msg.Chat.ID
	switch {
	case q.Data == cbAdminMain:
		b.showAdmin(ctx, chatID)
	case strings.HasPrefix(q.Data, cbAdminUsers):
		page, _ := strconv.Atoi(strings.TrimPrefix(q.Data, cbAdminUsers))
		b.showUsers(ctx, chatID, page)
	case q.Data == cbAdminSync:
		b.syncFromPanel(ctx, chatID)
	}
}

// syncFromPanel pulls the vault with a message while it runs, then sends
// the panel anew and the outcome under it. A clean outcome goes after
// syncShownFor; an error or lint findings stay to be read.
func (b *Bot) syncFromPanel(ctx context.Context, chatID int64) {
	wait := b.send(ctx, chatID, b.t.AdminSyncing, nil)
	b.later(ctx, func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, syncTimeout)
		defer cancel()
		r := b.SyncNow(ctx)
		if wait != nil {
			b.deleteMessage(ctx, chatID, wait.ID)
		}
		b.sendPanel(ctx, chatID)
		done := b.send(ctx, chatID, b.syncText(r), nil)
		if done != nil && r.Err == "" && r.Errors == 0 && r.Warnings == 0 {
			b.sleep(ctx, syncShownFor)
			b.deleteMessage(ctx, chatID, done.ID)
		}
	})
}

// panelMarkdown measures everything; what fails is logged and shown
// empty.
func (b *Bot) panelMarkdown(ctx context.Context) string {
	srv, err := b.AdminServer(ctx)
	if err != nil {
		b.log.Error("admin server", "err", err)
	}
	st, err := b.AdminStats(ctx)
	if err != nil {
		b.log.Error("admin stats", "err", err)
	}
	users, err := b.store.CountUsers(ctx, startOfDay(b.clock()))
	if err != nil {
		b.log.Error("admin users", "err", err)
	}
	B := stats.Bytes
	api := func(t store.Traffic) string { return fmt.Sprintf(b.t.AdminAPI, t.Calls, B(t.Sent), B(t.Received)) }
	cpu := strings.Replace(strconv.FormatFloat(srv.CPU, 'f', 1, 64), ".", ",", 1)
	sections := []string{
		"# " + b.t.AdminTitle,
		fmt.Sprintf(b.t.AdminUpdated, b.moment(b.clock())),
		fmt.Sprintf(b.t.AdminServerTable,
			fmt.Sprintf(b.t.AdminCPU, cpu, srv.Cores),
			fmt.Sprintf(b.t.AdminMemory, B(srv.RSS), B(srv.Heap), srv.Goroutines),
			fmt.Sprintf(b.t.AdminDisk, B(srv.DiskFree), B(srv.DiskTotal)),
			fmt.Sprintf(b.t.AdminNetwork, B(srv.NetRx), B(srv.NetTx)),
			api(srv.APIToday), api(srv.APITotal),
			b.duration(time.Duration(srv.Uptime)), escapeMarkdown(short(srv.Version))),
		b.storageTable(srv),
		fmt.Sprintf(b.t.AdminVaultTable, short(srv.Commit), b.ago(srv.CommitTime), srv.Notes, b.lastSyncCell(srv.LastSync)),
		fmt.Sprintf(b.t.AdminUsersTable, users.All, users.NewToday, users.New7d, users.Active24h, users.Active7d, users.Blocked),
		fmt.Sprintf(b.t.AdminReportsTable, st.Reports.Open, st.Reports.Accepted, st.Reports.Rejected,
			st.ReportsWeek.Open, st.ReportsWeek.Accepted, st.ReportsWeek.Rejected),
		fmt.Sprintf(b.t.AdminOpensTable, st.Opens.Today, st.Opens.Week, st.Opens.All),
		b.t.AdminTopWeek, b.notesTable(st.TopWeek),
		b.t.AdminTopAll, b.notesTable(st.TopAll),
		b.t.AdminSearchTitle,
		b.t.AdminSearchTop, b.searchTable(st.Searches),
		b.t.AdminSearchMissing, b.searchTable(st.Missing),
	}
	return strings.Join(sections, "\n\n") + "\n"
}

func (b *Bot) storageTable(s AdminServer) string {
	B := stats.Bytes
	r := s.Backup
	last := b.t.AdminBackupNone
	switch {
	case r.Err != "":
		last = fmt.Sprintf(b.t.AdminBackupFailed, b.moment(r.At), oneLine(r.Err))
	case !r.At.IsZero():
		last = b.moment(r.At) + ", " + B(r.Size)
	}
	next := b.t.AdminBackupUnsched
	if !r.Next.IsZero() {
		next = b.moment(r.Next)
	}
	vault := fmt.Sprintf(b.t.AdminVaultSize, B(s.Vault.Total), B(s.Vault.Markdown), B(s.Vault.Images), B(s.Vault.Git))
	return fmt.Sprintf(b.t.AdminStorageTable, vault, B(s.DataDir), B(s.DB), s.MediaRows,
		b.backupMode(r.Mode), escapeMarkdown(last), next)
}

func (b *Bot) lastSyncCell(r *SyncResult) string {
	if r == nil {
		return b.t.AdminSyncNever
	}
	outcome := b.t.AdminSyncSame
	switch {
	case r.Err != "":
		outcome = fmt.Sprintf(b.t.AdminSyncFailed, oneLine(r.Err))
	case r.Changed():
		outcome = short(r.OldCommit) + " → " + short(r.NewCommit)
	}
	return escapeMarkdown(fmt.Sprintf(b.t.AdminLastSync, b.moment(r.At), outcome))
}

// notesTable lists notes by title, with the folder they are in.
func (b *Bot) notesTable(notes []NoteStat) string {
	if len(notes) == 0 {
		return b.t.AdminNoOpens
	}
	lines := []string{b.t.AdminTopHead}
	for i, n := range notes {
		title := n.Title
		if n.Gone {
			title = fmt.Sprintf(b.t.AdminNoteGone, title)
		}
		folder := "—"
		if n.Path != "" {
			folder = strings.ReplaceAll(path.Dir(n.Path), "/", " / ")
		}
		lines = append(lines, fmt.Sprintf("| %d | %s | %s | %d |", i+1, escapeMarkdown(oneLine(title)),
			escapeMarkdown(folder), n.Opens))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) searchTable(list []store.SearchCount) string {
	if len(list) == 0 {
		return b.t.AdminNoSearches
	}
	lines := []string{b.t.AdminSearchHead}
	for i, c := range list {
		lines = append(lines, fmt.Sprintf("| %d | %s | %d |", i+1, escapeMarkdown(oneLine(c.Query)), c.Count))
	}
	return strings.Join(lines, "\n")
}

// showUsers sends a page of the user table; a page past the end shows
// the last one.
func (b *Bot) showUsers(ctx context.Context, chatID int64, page int) {
	l, err := b.AdminUsers(ctx, "", page)
	if err != nil {
		b.log.Error("admin users", "err", err)
	}
	var nav []models.InlineKeyboardButton
	if l.Page > 0 {
		nav = append(nav, button(b.t.AdminPrev, cbAdminUsers+strconv.Itoa(l.Page-1)))
	}
	if l.Page < l.Pages-1 {
		nav = append(nav, button(b.t.AdminNext, cbAdminUsers+strconv.Itoa(l.Page+1)))
	}
	var rows [][]models.InlineKeyboardButton
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, row(button(b.t.AdminBack, cbAdminMain)))
	b.showRich(ctx, chatID, b.t.AdminUsersTitle, b.usersMarkdown(l, b.topPlaces(ctx)), keyboard(rows...))
}

// topPlaces is each user's place in /top as everyone sees it: users with
// accepted reports who do not hide, up to the size of the top.
func (b *Bot) topPlaces(ctx context.Context) map[int64]int {
	all, err := b.store.Leaderboard(ctx)
	if err != nil {
		b.log.Error("leaderboard", "err", err)
	}
	size, places := b.settings.Get().Top.Size, map[int64]int{}
	for _, e := range all {
		if !e.HideTop && len(places) < size {
			places[e.TgID] = len(places) + 1
		}
	}
	return places
}

// usersMarkdown renders a page of the user table; places are from
// topPlaces.
func (b *Bot) usersMarkdown(l AdminUsers, places map[int64]int) string {
	md := "# " + b.t.AdminUsersTitle + "\n\n" + fmt.Sprintf(b.t.AdminUsersPage, l.Page+1, l.Pages, l.Found) + "\n\n"
	if len(l.Users) == 0 {
		return md + b.t.AdminNobody + "\n"
	}
	yesNo := func(v bool) string {
		if v {
			return b.t.AdminYes
		}
		return b.t.AdminNo
	}
	cell := func(s string) string {
		if s == "" {
			return "—"
		}
		return escapeMarkdown(oneLine(s))
	}
	lines := []string{b.t.AdminUsersHead}
	for i, u := range l.Users {
		username, top := "", b.t.AdminNo
		if u.Username != "" {
			username = "@" + u.Username
		}
		switch p, ok := places[u.ID]; {
		case ok:
			top = fmt.Sprintf(b.t.AdminTopPlace, p)
		case u.HideTop && u.Accepted > 0:
			top = b.t.AdminHidden
		}
		lines = append(lines, fmt.Sprintf("| %d | %d | %s | %s | %s | %s | %d | %d | %d | %s | %s | %s | %s |",
			l.Page*usersPerPage+i+1, u.ID, cell(username), cell(u.FirstName), cell(u.Nickname), cell(u.Rank),
			u.Accepted, u.Open, u.Rejected, b.stamp(u.FirstSeen), b.stamp(u.LastSeen), top, yesNo(u.Blocked)))
	}
	return md + strings.Join(lines, "\n") + "\n"
}

// stamp is a compact date and time for tables: «20.10.2026 14:05».
func (b *Bot) stamp(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("02.01.2006 15:04")
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
