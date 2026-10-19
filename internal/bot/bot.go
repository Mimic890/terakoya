// Package bot implements the Telegram side: catalog navigation and article
// delivery.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/media"
	"github.com/Mimic890/terakoya/internal/ranks"
	"github.com/Mimic890/terakoya/internal/raster"
	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/stats"
	"github.com/Mimic890/terakoya/internal/store"
)

// Version is the bot's version or commit, set at build time with
// -ldflags "-X github.com/Mimic890/terakoya/internal/bot.Version=…".
var Version = "dev"

const (
	// seenEvery throttles recording when a user was last seen.
	seenEvery = 5 * time.Minute
	// trafficEvery is how often the Telegram API traffic is stored.
	trafficEvery = time.Minute
)

// settingsCheck is how often the settings file is checked for hand edits.
const settingsCheck = 5 * time.Second

// ladder is the rank ladder in use.
func (b *Bot) ladder() ranks.Ladder { return b.settings.Get().Ladder() }

// Bot serves the catalog over Telegram.
type Bot struct {
	api   *tg.Bot
	cfg   config.Config
	store *store.Store
	repo  *gitrepo.Repo
	media *media.Resolver
	host  githost.Host // nil when suggestions are off
	log   *slog.Logger
	t     i18n.Strings

	emoji       *emojiSet
	settings    *settings.File
	msgs        *tracker
	bg          sync.WaitGroup // work left running after a reply, such as deleting old messages
	userLocks   userLocks
	syncMu      sync.Mutex
	rejectGrace time.Duration
	// Broadcast pacing; tests shorten them and record the waits.
	broadcastGap  time.Duration
	progressEvery time.Duration
	sleep         func(context.Context, time.Duration)

	clock    func() time.Time
	started  time.Time
	traffic  *stats.Counter // calls to the Telegram API
	seenMu   sync.Mutex
	seen     map[int64]time.Time // when a user's last_seen_at was last written
	adminMu  sync.Mutex
	views    map[int64]adminView // each admin's place in /admin
	sizes    sizeCache
	procRoot string // /proc and /sys, replaced in tests
	sysRoot  string

	mu       sync.RWMutex
	cat      *content.Catalog
	sha      string
	lastSync *SyncResult
}

// New creates the bot and loads the catalog.
// Extra options are passed to the Telegram client.
func New(ctx context.Context, cfg config.Config, st *store.Store, repo *gitrepo.Repo, host githost.Host, log *slog.Logger, opts ...tg.Option) (*Bot, error) {
	b := &Bot{cfg: cfg, store: st, repo: repo, host: host, log: log, t: i18n.RU, msgs: &tracker{store: st, log: log},
		rejectGrace: rejectGrace, broadcastGap: broadcastGap, progressEvery: progressEvery, sleep: sleep,
		clock: time.Now, traffic: &stats.Counter{}, seen: map[int64]time.Time{}, views: map[int64]adminView{},
		procRoot: "/proc", sysRoot: "/sys"}
	b.started = b.clock()
	api, err := tg.New(cfg.BotToken, append([]tg.Option{
		// Like the library's own client, with the traffic counted.
		tg.WithHTTPClient(time.Minute, &http.Client{Timeout: time.Minute, Transport: b.traffic}),
		tg.WithDefaultHandler(b.handle),
		tg.WithAllowedUpdates(tg.AllowedUpdates{"message", "callback_query", "my_chat_member"}),
		tg.WithErrorsHandler(func(err error) { log.Error("telegram", "err", err) }),
	}, opts...)...)
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
	b.emoji = newEmojiSet(log)
	b.media = &media.Resolver{Cache: st, Sender: api, CacheChatID: cfg.CacheChatID,
		Raster: raster.New(cfg.Resvg, cfg.FontsDir, cfg.RepoDir), Log: log}
	if err := b.Reload(ctx); err != nil {
		return nil, err
	}
	var subjects []string
	for _, s := range b.catalog().Subjects {
		subjects = append(subjects, s.Name)
	}
	b.settings, err = settings.Open(cfg.SettingsPath(), subjects, cfg.LegacyRanks, log)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	b.settings.OnChange = func(s settings.Settings) {
		b.emoji.set(s)
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		b.emoji.check(cctx, api)
	}
	b.emoji.set(b.settings.Get())
	b.emoji.check(ctx, api)
	return b, nil
}

// Run polls Telegram for updates and syncs content until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	b.log.Info("bot started", "username", b.cfg.BotUsername, "reports", b.host != nil, "admins", len(b.cfg.Admins))
	go b.syncLoop(ctx)
	go b.settings.Watch(ctx, settingsCheck)
	go b.trafficLoop(ctx)
	b.setCommands(ctx)
	b.api.Start(ctx)
	b.storeTraffic(context.WithoutCancel(ctx))
}

// trafficLoop stores the Telegram API traffic every minute.
func (b *Bot) trafficLoop(ctx context.Context) {
	t := time.NewTicker(trafficEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.storeTraffic(ctx)
		}
	}
}

// storeTraffic adds the traffic since the last call to today's total.
func (b *Bot) storeTraffic(ctx context.Context) {
	t := b.traffic.Take()
	if t == (store.Traffic{}) {
		return
	}
	if err := b.store.AddTraffic(ctx, store.Day(b.clock()), t); err != nil {
		b.log.Error("store traffic", "err", err)
	}
}

// markSeen records that a user did something, at most once per
// seenEvery, in the background: the user waits for the answer only.
func (b *Bot) markSeen(ctx context.Context, u *models.User) {
	if u == nil || u.IsBot {
		return
	}
	now := b.clock()
	b.seenMu.Lock()
	last, ok := b.seen[u.ID]
	due := !ok || now.Sub(last) >= seenEvery
	if due {
		b.seen[u.ID] = now
	}
	b.seenMu.Unlock()
	if !due {
		return
	}
	id, username, first := u.ID, u.Username, u.FirstName
	b.later(ctx, func(ctx context.Context) {
		if err := b.store.Seen(ctx, id, username, first); err != nil {
			b.log.Error("record last seen", "err", err)
		}
	})
}

// Reload reads the catalog from disk and swaps it in.
func (b *Bot) Reload(ctx context.Context) error {
	_, err := b.reload(ctx)
	return err
}

// reload is Reload, returning the findings about the content.
func (b *Bot) reload(ctx context.Context) ([]content.Issue, error) {
	cat, issues, err := content.Load(b.cfg.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("load content: %w", err)
	}
	for _, i := range issues {
		b.log.Warn("content", "issue", i.String())
	}
	sha := b.repo.HeadSHA(ctx)
	b.mu.Lock()
	b.cat, b.sha = cat, sha
	b.mu.Unlock()
	b.log.Info("vault loaded", "notes", len(cat.Notes()), "commit", sha)
	return issues, nil
}

func (b *Bot) catalog() *content.Catalog {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cat
}

func (b *Bot) handle(ctx context.Context, _ *tg.Bot, u *models.Update) {
	switch {
	case u.CallbackQuery != nil:
		b.markSeen(ctx, &u.CallbackQuery.From)
	case u.Message != nil && u.Message.Chat.Type == models.ChatTypePrivate:
		b.markSeen(ctx, u.Message.From)
	}
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
	// An admin in /emoji_setup answers it; otherwise an admin collecting
	// custom emoji IDs gets them, and nothing else.
	if b.onEmojiWizardMessage(ctx, m) || b.onEmojiIDs(ctx, m) {
		return
	}
	// The bottom keyboard works in the middle of a dialog too; the dialog
	// stays where it was.
	if b.onKeyButton(ctx, m) {
		return
	}
	cmd, arg := splitCommand(m.Text)
	if cmd != "" {
		// Typed commands do not stay in the chat once answered.
		defer b.deleteMessage(ctx, m.Chat.ID, m.ID)
	}
	if b.onAdminCommand(ctx, m, cmd, arg) {
		return
	}
	switch cmd {
	case "/cancel":
		b.cancelDialog(ctx, m.Chat.ID, m.From.ID)
		return
	case "/start":
		// Navigating away abandons a pending suggestion.
		if err := b.store.DeleteSession(ctx, m.From.ID); err != nil {
			b.log.Error("delete session", "err", err)
		}
	case "":
		if b.onSessionMessage(ctx, m) {
			return
		}
	}
	switch cmd {
	case "/download":
		b.sendArchive(ctx, m.Chat.ID)
	case "/top":
		b.showTop(ctx, m.Chat.ID, m.From)
	case "/ranks":
		b.showRanks(ctx, m.Chat.ID, m.From)
	case "/start":
		b.greet(ctx, m.Chat.ID, m.From)
		if arg != "" {
			if n := b.catalog().Note(arg); n != nil {
				b.sendArticle(ctx, m.Chat.ID, n, false)
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
	var msg *models.Message
	err := b.withPlain(kb, func(kb *models.InlineKeyboardMarkup) error {
		p := &tg.SendMessageParams{ChatID: chatID, Text: text}
		if kb != nil {
			p.ReplyMarkup = kb
		}
		var err error
		msg, err = b.api.SendMessage(ctx, p)
		return err
	})
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
// the message can no longer be edited. A rich menu is replaced by the new
// one.
func (b *Bot) editMenu(ctx context.Context, chatID int64, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if b.msgs.isRich(ctx, chatID, msgID) {
		b.sendMenu(ctx, chatID, text, kb)
		b.retireLater(ctx, chatID, msgID)
		return
	}
	err := b.withPlain(kb, func(kb *models.InlineKeyboardMarkup) error {
		_, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{
			ChatID: chatID, MessageID: msgID, Text: text, ReplyMarkup: kb,
		})
		if err != nil && strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		return err
	})
	if err == nil {
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

// backgroundKey marks the context of work started with later.
type backgroundKey struct{}

// inBackground reports whether ctx is that of work started with later.
func inBackground(ctx context.Context) bool { return ctx.Value(backgroundKey{}) != nil }

// later runs f in the background, after the handler has answered. It
// keeps running when the update's context ends.
func (b *Bot) later(ctx context.Context, f func(context.Context)) {
	ctx = context.WithValue(context.WithoutCancel(ctx), backgroundKey{}, true)
	b.bg.Add(1)
	go func() {
		defer b.bg.Done()
		f(ctx)
	}()
}

// retireLater removes a replaced message in the background.
func (b *Bot) retireLater(ctx context.Context, chatID int64, msgID int) {
	b.later(ctx, func(ctx context.Context) {
		b.retire(ctx, &models.Message{ID: msgID, Chat: models.Chat{ID: chatID}})
	})
}

func (b *Bot) answer(ctx context.Context, q *models.CallbackQuery, text string, alert bool) {
	_, err := b.api.AnswerCallbackQuery(ctx, &tg.AnswerCallbackQueryParams{
		CallbackQueryID: q.ID, Text: text, ShowAlert: alert,
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		b.log.Debug("answer callback", "err", err)
	}
}
