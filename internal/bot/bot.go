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

	mu  sync.RWMutex
	cat *content.Catalog
	sha string
}

// New creates the bot and loads the catalog.
func New(ctx context.Context, cfg config.Config, st *store.Store, repo *gitrepo.Repo, log *slog.Logger) (*Bot, error) {
	b := &Bot{cfg: cfg, store: st, repo: repo, log: log, t: i18n.RU}
	api, err := tg.New(cfg.BotToken,
		tg.WithDefaultHandler(b.handle),
		tg.WithAllowedUpdates(tg.AllowedUpdates{"message", "callback_query"}),
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
	case u.Message != nil && u.Message.From != nil && u.Message.Chat.Type == models.ChatTypePrivate:
		b.onMessage(ctx, u.Message)
	}
}

func (b *Bot) onMessage(ctx context.Context, m *models.Message) {
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
		b.send(ctx, m.Chat.ID, text, kb)
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
	}
	return msg
}

// edit replaces a navigation message in place.
func (b *Bot) edit(ctx context.Context, msg *models.Message, text string, kb *models.InlineKeyboardMarkup) {
	_, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, Text: text, ReplyMarkup: kb,
	})
	if err == nil || strings.Contains(err.Error(), "message is not modified") {
		return
	}
	// The message may be too old to edit; send a fresh one instead.
	b.log.Debug("edit message", "err", err)
	b.send(ctx, msg.Chat.ID, text, kb)
}

func (b *Bot) answer(ctx context.Context, q *models.CallbackQuery, text string, alert bool) {
	_, err := b.api.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{
		CallbackQueryID: q.ID, Text: text, ShowAlert: alert,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		b.log.Debug("answer callback", "err", err)
	}
}
