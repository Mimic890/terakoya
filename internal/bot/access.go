package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/store"
)

// Edit access: «Исправить текст», «Добавить тему» and «Добавить теорему,
// задачу…» need an admin's permission; «Сообщить об ошибке» does not.

// accessCooldown is how long a denied (or revoked) user waits before asking
// again.
const accessCooldown = 7 * 24 * time.Hour

const (
	cbRequest = "ar:" // ar:<note id>, from the «нужно разрешение» prompt
	cbAllow   = "ay:" // ay:<user id>
	cbDeny    = "an:" // an:<user id>
	cbClose   = "c"   // removes the message it is pressed in
)

// mayEdit reports whether the user may make edits.
func (b *Bot) mayEdit(ctx context.Context, tgID int64) bool {
	if b.cfg.IsAdmin(tgID) {
		return true
	}
	a, err := b.store.Access(ctx, tgID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		b.log.Error("access", "err", err)
	}
	return err == nil && a.Status == store.AccessGranted
}

// gatedState reports whether a dialog state belongs to an edit, which
// ends if access is revoked meanwhile.
func gatedState(state string) bool {
	switch state {
	case stateMenu, stateReport, stateReportConfirm:
		return false
	}
	return true
}

// accessBlock says why the user cannot ask for access right now: a toast,
// or a date until which a denial holds. Both are empty if they can ask.
func (b *Bot) accessBlock(ctx context.Context, tgID int64) (toast string, until time.Time) {
	if len(b.cfg.Admins) == 0 {
		return b.t.AccessNoAdmins, time.Time{}
	}
	a, err := b.store.Access(ctx, tgID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "", time.Time{}
	case err != nil:
		b.log.Error("access", "err", err)
		return b.t.SubmitFailed, time.Time{}
	case a.Status == store.AccessPending:
		return b.t.AccessPending, time.Time{}
	case a.Status == store.AccessGranted:
		return b.t.AccessHave, time.Time{}
	}
	if until := a.DecidedAt.Add(accessCooldown); time.Now().Before(until) {
		return "", until
	}
	return "", time.Time{}
}

// askAccess answers a tap on an edit button by a user without access. The
// question replaces the dialog message msgID, or is sent anew when msgID is
// 0; noteID is the note to come back to.
func (b *Bot) askAccess(ctx context.Context, q *models.CallbackQuery, chatID int64, msgID int, noteID string) {
	toast, until := b.accessBlock(ctx, q.From.ID)
	if toast != "" {
		b.answer(ctx, q, toast, true)
		return
	}
	b.answer(ctx, q, "", false)
	text := b.t.AccessNeeded
	kb := keyboard(row(button(b.t.SendRequest, cbRequest+noteID), button(b.t.Cancel, cbClose)))
	if !until.IsZero() {
		text = fmt.Sprintf(b.t.AccessDeniedUntil, b.date(until))
		kb = keyboard(row(button(b.t.Understood, cbClose)))
	}
	if msgID == 0 {
		b.send(ctx, chatID, text, kb)
		return
	}
	// The dialog's menu becomes the question; the dialog is over.
	if err := b.store.DeleteSession(ctx, q.From.ID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	if _, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
		ChatID: chatID, MessageID: msgID, Text: text, ReplyMarkup: kb,
	}); err != nil {
		b.log.Debug("edit message", "err", err)
		b.send(ctx, chatID, text, kb)
	}
}

// requestAccess sends the user's request to every admin.
func (b *Bot) requestAccess(ctx context.Context, q *models.CallbackQuery, msg *models.Message, noteID string) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	if b.mayEdit(ctx, q.From.ID) {
		b.answer(ctx, q, b.t.AccessHave, true)
		return
	}
	toast, until := b.accessBlock(ctx, q.From.ID)
	if toast == "" && !until.IsZero() {
		toast = fmt.Sprintf(b.t.AccessDeniedUntil, b.date(until))
	}
	if toast != "" {
		b.answer(ctx, q, toast, true)
		return
	}
	u, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
		b.answer(ctx, q, b.t.SubmitFailed, true)
		return
	}
	edits, reports, err := b.store.AcceptedCounts(ctx, u.TgID)
	if err != nil {
		b.log.Error("accepted counts", "err", err)
	}
	text := fmt.Sprintf(b.t.AdminRequest, requester(u), edits, reports)
	if err := b.store.RequestAccess(ctx, u.TgID, noteID, text); err != nil {
		b.log.Error("request access", "err", err)
		b.answer(ctx, q, b.t.SubmitFailed, true)
		return
	}
	id := strconv.FormatInt(u.TgID, 10)
	kb := keyboard(row(button(b.t.Allow, cbAllow+id), button(b.t.Deny, cbDeny+id)))
	delivered := 0
	for _, admin := range b.cfg.Admins {
		m, err := b.api.SendMessage(ctx, &tg.SendMessageParams{ChatID: admin, Text: text, ReplyMarkup: kb})
		if err != nil {
			// An admin who never opened the bot cannot be written to.
			b.log.Warn("send access request to admin", "admin", admin, "err", err)
			continue
		}
		delivered++
		b.msgs.seen(ctx, admin, m.ID)
		if err := b.store.PutAccessMessage(ctx, u.TgID, admin, m.ID); err != nil {
			b.log.Error("store access message", "err", err)
		}
	}
	b.log.Info("access requested", "user", u.TgID, "admins", delivered)
	b.answer(ctx, q, "", false)
	if _, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, Text: b.t.AccessRequested, ReplyMarkup: keyboard(),
	}); err != nil {
		b.log.Debug("edit message", "err", err)
		b.send(ctx, msg.Chat.ID, b.t.AccessRequested, nil)
	}
}

// decideAccess handles «Разрешить» and «Отклонить». The first admin to
// tap decides; every admin's copy then shows who it was.
func (b *Bot) decideAccess(ctx context.Context, q *models.CallbackQuery, msg *models.Message, data string) {
	if !b.cfg.IsAdmin(q.From.ID) {
		b.answer(ctx, q, "", false)
		return
	}
	granted := strings.HasPrefix(data, cbAllow)
	userID, err := strconv.ParseInt(data[len(cbAllow):], 10, 64)
	if err != nil {
		b.answer(ctx, q, "", false)
		return
	}
	// The admin's name is shown to the other admins and in /access.
	if _, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	ok, err := b.store.DecideAccess(ctx, userID, granted, q.From.ID)
	if err != nil {
		b.log.Error("decide access", "err", err)
		b.answer(ctx, q, b.t.SubmitFailed, true)
		return
	}
	a, err := b.store.Access(ctx, userID)
	if err != nil {
		b.log.Error("access", "err", err)
		b.answer(ctx, q, "", false)
		b.dropButtonsOf(ctx, msg)
		return
	}
	if !ok {
		by := b.userName(ctx, a.DecidedBy)
		b.answer(ctx, q, fmt.Sprintf(b.t.AdminDecided, by), true)
		b.editAdminCopies(ctx, a)
		return
	}
	b.answer(ctx, q, "", false)
	b.log.Info("access decided", "user", userID, "granted", granted, "admin", q.From.ID)
	b.editAdminCopies(ctx, a)
	if granted {
		var kb *models.InlineKeyboardMarkup
		if n := b.catalog().Note(a.NoteID); n != nil {
			kb = keyboard(row(button(fmt.Sprintf(b.t.OpenNamed, shorten(n.Title, maxLabelRunes)), cbOpen+n.ID)))
		}
		b.send(ctx, userID, b.t.AccessGranted, kb)
	} else {
		b.send(ctx, userID, b.t.AccessDenied, nil)
	}
}

// editAdminCopies shows the outcome of a request on every admin's copy and
// removes its buttons.
func (b *Bot) editAdminCopies(ctx context.Context, a store.Access) {
	line := b.t.AdminDenied
	if a.Status == store.AccessGranted {
		line = b.t.AdminAllowed
	}
	text := a.RequestText + "\n\n" + fmt.Sprintf(line, b.userName(ctx, a.DecidedBy))
	msgs, err := b.store.AccessMessages(ctx, a.TgID)
	if err != nil {
		b.log.Error("access messages", "err", err)
		return
	}
	for admin, id := range msgs {
		_, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
			ChatID: admin, MessageID: id, Text: text, ReplyMarkup: keyboard(),
		})
		if err != nil && !strings.Contains(err.Error(), "message is not modified") {
			b.log.Debug("edit admin copy", "admin", admin, "err", err)
		}
	}
}

// onAdminCommand handles /access and /revoke. It reports false for other
// commands and for users who are not admins, who get no answer at all.
func (b *Bot) onAdminCommand(ctx context.Context, m *models.Message, cmd, arg string) bool {
	if cmd != "/access" && cmd != "/revoke" {
		return false
	}
	if !b.cfg.IsAdmin(m.From.ID) {
		return true
	}
	if cmd == "/access" {
		b.send(ctx, m.Chat.ID, b.accessList(ctx), nil)
		return true
	}
	if arg == "" {
		b.send(ctx, m.Chat.ID, b.t.RevokeUsage, nil)
		return true
	}
	var u store.User
	var err error
	if id, perr := strconv.ParseInt(arg, 10, 64); perr == nil {
		u, err = b.store.User(ctx, id)
	} else {
		u, err = b.store.UserByUsername(ctx, arg)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		b.send(ctx, m.Chat.ID, b.t.RevokeUnknown, nil)
		return true
	case err != nil:
		b.log.Error("find user", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return true
	case b.cfg.IsAdmin(u.TgID):
		b.send(ctx, m.Chat.ID, b.t.RevokeAdmin, nil)
		return true
	}
	ok, err := b.store.RevokeAccess(ctx, u.TgID, m.From.ID)
	switch {
	case err != nil:
		b.log.Error("revoke access", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
	case !ok:
		b.send(ctx, m.Chat.ID, b.t.RevokeNoAccess, nil)
	default:
		b.log.Info("access revoked", "user", u.TgID, "admin", m.From.ID)
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.RevokeDone, who(u)), nil)
		b.send(ctx, u.TgID, b.t.AccessRevoked, nil)
	}
	return true
}

func (b *Bot) accessList(ctx context.Context) string {
	var sb strings.Builder
	grants, err := b.store.Grants(ctx)
	if err != nil {
		b.log.Error("grants", "err", err)
		return b.t.SubmitFailed
	}
	if len(grants) == 0 {
		sb.WriteString(b.t.AccessListEmpty)
	} else {
		people := "человек"
		if n := len(grants); n%10 == 1 && n%100 != 11 {
			people = "человека"
		}
		fmt.Fprintf(&sb, b.t.AccessList, len(grants), people)
		for _, g := range grants {
			by := g.DecidedBy
			name := who(by)
			if by.FirstName == "" && by.Username == "" {
				name = strconv.FormatInt(by.TgID, 10)
			}
			sb.WriteString("\n")
			fmt.Fprintf(&sb, b.t.AccessListLine, who(g.User)+", "+strconv.FormatInt(g.TgID, 10), g.Since.Format("02.01"), name)
		}
	}
	var admins []string
	for _, id := range b.cfg.Admins {
		admins = append(admins, b.userName(ctx, id))
	}
	if len(admins) > 0 {
		sb.WriteString("\n\n")
		fmt.Fprintf(&sb, b.t.AccessAdmins, strings.Join(admins, ", "))
	}
	return sb.String()
}

// who names a user as «Маша (@masha)», or just «Маша».
func who(u store.User) string {
	name := u.FirstName
	switch {
	case u.Username == "":
	case name == "":
		name = "@" + u.Username
	default:
		name += " (@" + u.Username + ")"
	}
	return name
}

// requester describes a user for the admins: «Маша · @masha · ID 123».
func requester(u store.User) string {
	parts := []string{}
	if u.FirstName != "" {
		parts = append(parts, u.FirstName)
	}
	if u.Username != "" {
		parts = append(parts, "@"+u.Username)
	}
	return strings.Join(append(parts, "ID "+strconv.FormatInt(u.TgID, 10)), " · ")
}

// userName names a known user, or gives their ID.
func (b *Bot) userName(ctx context.Context, id int64) string {
	if u, err := b.store.User(ctx, id); err == nil {
		if s := who(u); s != "" {
			return s
		}
	}
	return strconv.FormatInt(id, 10)
}

// date formats a day as «25 октября».
func (b *Bot) date(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), b.t.Months[t.Month()-1])
}
