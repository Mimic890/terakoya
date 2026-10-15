// Package bot implements the Telegram side: catalog navigation and article
// delivery.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/media"
	"github.com/Mimic890/terakoya/internal/store"
)

// Bot serves the catalog over Telegram.
type Bot struct {
	api   *tg.Bot
	cfg   config.Config
	store *store.Store
	repo  *gitrepo.Repo
	media *media.Resolver
	log   *slog.Logger
	t     i18n.Strings

	msgs *tracker

	mu  sync.RWMutex
	cat *content.Catalog
	sha string
}

// New creates the bot and loads the catalog.
func New(ctx context.Context, cfg config.Config, st *store.Store, repo *gitrepo.Repo, log *slog.Logger) (*Bot, error) {
	b := &Bot{cfg: cfg, store: st, repo: repo, log: log, t: i18n.RU, msgs: &tracker{store: st, log: log}}
	api, err := tg.New(cfg.BotToken,
		tg.WithDefaultHandler(b.handle),
		tg.WithAllowedUpdates(tg.AllowedUpdates{"message", "callback_query", "my_chat_member"}),
		tg.WithErrorsHandler(func(err error) { log.Error("telegram", "err", err) }),
	)
	if err != nil {
		return nil, err
	}
	b.api = api
	if b.cfg.BotUsername == "" {
		me, err := api.GetMe(ctx)
		if err != nil {
			return nil, fmt.Errorf("getMe: %w", err)
		}
		b.cfg.BotUsername = me.Username
	}
	b.media = &media.Resolver{Cache: st, Sender: api, CacheChatID: cfg.CacheChatID, Log: log}
	if err := b.Reload(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// Run polls Telegram for updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	b.log.Info("bot started", "username", b.cfg.BotUsername)
	b.api.Start(ctx)
}

// Reload reads the catalog from disk and swaps it in.
func (b *Bot) Reload(ctx context.Context) error {
	cat, issues, err := content.Load(b.cfg.ContentDir())
	if err != nil {
		return fmt.Errorf("load content: %w", err)
	}
	for _, i := range issues {
		b.log.Warn("content", "issue", i.String())
	}
	sha := b.repo.HeadSHA(ctx)
	b.mu.Lock()
	b.cat, b.sha = cat, sha
	b.mu.Unlock()
	b.log.Info("catalog loaded", "articles", len(cat.Articles()), "commit", sha)
	return nil
}

func (b *Bot) catalog() *content.Catalog {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cat
}

func (b *Bot) handle(ctx context.Context, _ *tg.Bot, u *models.Update) {
	switch {
	case u.CallbackQuery != nil:
		b.onCallback(ctx, u.CallbackQuery)
	case u.MyChatMember != nil:
		m := u.MyChatMember
		b.log.Info("bot membership changed; use chat_id as TERAKOYA_CACHE_CHAT_ID for a cache chat",
			"chat_id", m.Chat.ID, "title", m.Chat.Title, "type", m.Chat.Type, "status", m.NewChatMember.Type)
	case u.Message != nil && u.Message.Chat.Type != models.ChatTypePrivate:
		b.logGroupMessage(u.Message)
	case u.Message != nil && u.Message.From != nil:
		b.onMessage(ctx, u.Message)
	}
}

// logGroupMessage reports the chat ID of groups the bot is in, so a group can
// be configured as the file_id cache chat. The bot does not reply in groups.
func (b *Bot) logGroupMessage(m *models.Message) {
	if m.MigrateToChatID != 0 {
		b.log.Info("group was upgraded to a supergroup; use the new chat_id",
			"old_chat_id", m.Chat.ID, "chat_id", m.MigrateToChatID, "title", m.Chat.Title)
		return
	}
	b.log.Info("message in group; use chat_id as TERAKOYA_CACHE_CHAT_ID for a cache chat",
		"chat_id", m.Chat.ID, "title", m.Chat.Title, "type", m.Chat.Type)
}

func (b *Bot) onMessage(ctx context.Context, m *models.Message) {
	b.msgs.seen(ctx, m.Chat.ID, m.ID)
	cmd, arg := splitCommand(m.Text)
	switch cmd {
	case "/start":
		if arg != "" {
			if a := b.catalog().Article(arg); a != nil {
				b.sendArticle(ctx, m.Chat.ID, a)
				return
			}
			b.send(ctx, m.Chat.ID, b.t.NotFound, nil)
		}
		text, kb := b.homeView(ctx, m.From)
		b.sendMenu(ctx, m.Chat.ID, text, kb)
	default:
		b.send(ctx, m.Chat.ID, b.t.UnknownCommand, nil)
	}
}

// splitCommand parses "/cmd@bot arg".
func splitCommand(text string) (cmd, arg string) {
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	cmd, arg, _ = strings.Cut(strings.TrimSpace(text), " ")
	cmd, _, _ = strings.Cut(cmd, "@")
	return strings.ToLower(cmd), strings.TrimSpace(arg)
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, kb *models.InlineKeyboardMarkup) *models.Message {
	p := &tg.SendMessageParams{ChatID: chatID, Text: text}
	if kb != nil {
		p.ReplyMarkup = kb
	}
	msg, err := b.api.SendMessage(ctx, p)
	if err != nil {
		b.log.Error("send message", "chat", chatID, "err", err)
		return nil
	}
	b.msgs.seen(ctx, chatID, msg.ID)
	return msg
}

// sendMenu sends a new navigation menu.
func (b *Bot) sendMenu(ctx context.Context, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	if msg := b.send(ctx, chatID, text, kb); msg != nil {
		b.msgs.setMenu(ctx, chatID, msg.ID)
	}
}

// editMenu replaces a navigation message in place, or sends a new menu if
// the message can no longer be edited.
func (b *Bot) editMenu(ctx context.Context, chatID int64, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	_, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
		ChatID: chatID, MessageID: msgID, Text: text, ReplyMarkup: kb,
	})
	if err == nil || strings.Contains(err.Error(), "message is not modified") {
		b.msgs.setMenu(ctx, chatID, msgID)
		return
	}
	b.log.Debug("edit message", "err", err)
	b.sendMenu(ctx, chatID, text, kb)
}

// showMenu edits the current menu if it is still the last message in the
// chat, and sends a new one otherwise.
func (b *Bot) showMenu(ctx context.Context, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	if id := b.msgs.lastMenu(ctx, chatID); id != 0 {
		b.editMenu(ctx, chatID, id, text, kb)
		return
	}
	b.sendMenu(ctx, chatID, text, kb)
}

// retire removes a message that has been replaced by a newer one. Telegram
// refuses to delete messages older than 48 hours; those lose their keyboard
// instead, so their buttons cannot be used any more.
func (b *Bot) retire(ctx context.Context, msg *models.Message) {
	chatID := msg.Chat.ID
	_, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: msg.ID})
	if err == nil {
		b.msgs.gone(ctx, chatID, msg.ID)
		return
	}
	b.log.Debug("delete message failed; removing its keyboard", "err", err)
	b.msgs.dropMenu(ctx, chatID, msg.ID)
	_, err = b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
		ChatID: chatID, MessageID: msg.ID, ReplyMarkup: keyboard(),
	})
	if err != nil {
		b.log.Debug("clear keyboard", "err", err)
	}
}

func (b *Bot) answer(ctx context.Context, q *models.CallbackQuery, text string, alert bool) {
	_, err := b.api.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{
		CallbackQueryID: q.ID, Text: text, ShowAlert: alert,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		b.log.Debug("answer callback", "err", err)
	}
}
