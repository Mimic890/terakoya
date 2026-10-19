package bot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/stats"
	"github.com/Mimic890/terakoya/internal/store"
)

// The numbers behind /admin, shared with the admin socket and its CLI.

// usersPerPage is the length of a page of the user list.
const usersPerPage = 10

// sizesFor is how long directory sizes are reused.
const sizesFor = 60 * time.Second

// AdminStatus is the summary of /admin's main screen.
type AdminStatus struct {
	Users       int       `json:"users"`
	Active24h   int       `json:"active_24h"`
	OpenReports int       `json:"open_reports"`
	Commit      string    `json:"commit"`
	CommitTime  time.Time `json:"commit_time"`
	Uptime      Duration  `json:"uptime"`
}

// Duration marshals as whole seconds.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(time.Duration(d)/time.Second), 10)), nil
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseInt(string(b), 10, 64)
	*d = Duration(time.Duration(n) * time.Second)
	return err
}

// AdminStatus summarizes the bot.
func (b *Bot) AdminStatus(ctx context.Context) (AdminStatus, error) {
	c, err := b.store.CountUsers(ctx, startOfDay(b.clock()))
	if err != nil {
		return AdminStatus{}, err
	}
	open, err := b.store.OpenCount(ctx)
	if err != nil {
		return AdminStatus{}, err
	}
	b.mu.RLock()
	sha := b.sha
	b.mu.RUnlock()
	return AdminStatus{Users: c.All, Active24h: c.Active24h, OpenReports: open, Commit: sha,
		CommitTime: b.repo.CommitTime(ctx, sha), Uptime: Duration(b.clock().Sub(b.started))}, nil
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// AdminServer is the «Сервер» screen.
type AdminServer struct {
	RSS        int64    `json:"rss"`
	Heap       int64    `json:"heap"`
	Goroutines int      `json:"goroutines"`
	Uptime     Duration `json:"uptime"`
	Version    string   `json:"version"`
	// NetRx and NetTx are the container's network bytes since it started.
	NetRx    int64         `json:"net_rx"`
	NetTx    int64         `json:"net_tx"`
	APIToday store.Traffic `json:"api_today"`
	APITotal store.Traffic `json:"api_total"`
	Vault    stats.Vault   `json:"vault"`
	DataDir  int64         `json:"data_dir"`
	DB       int64         `json:"db"`
	// MediaRows is the number of cached Telegram file_ids.
	MediaRows  int         `json:"media_rows"`
	Commit     string      `json:"commit"`
	CommitTime time.Time   `json:"commit_time"`
	Notes      int         `json:"notes"`
	LastSync   *SyncResult `json:"last_sync,omitempty"`
}

// sizeCache keeps directory sizes for sizesFor.
type sizeCache struct {
	mu    sync.Mutex
	at    time.Time
	vault stats.Vault
	data  int64
}

// diskSizes walks the vault and the data directory, at most once per
// sizesFor.
func (b *Bot) diskSizes(ctx context.Context) (stats.Vault, int64, error) {
	c := &b.sizes
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && b.clock().Sub(c.at) < sizesFor {
		return c.vault, c.data, nil
	}
	type result struct {
		v    stats.Vault
		data int64
		err  error
	}
	done := make(chan result, 1)
	go func() {
		v, err := stats.VaultSize(b.cfg.RepoDir)
		data, err2 := stats.DirSize(b.cfg.DataDir)
		done <- result{v, data, errors.Join(err, err2)}
	}()
	select {
	case <-ctx.Done():
		return stats.Vault{}, 0, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return r.v, r.data, r.err
		}
		c.at, c.vault, c.data = b.clock(), r.v, r.data
		return r.v, r.data, nil
	}
}

// AdminServer measures the process, the disk and the traffic.
func (b *Bot) AdminServer(ctx context.Context) (AdminServer, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s := AdminServer{Heap: int64(ms.HeapAlloc), Goroutines: runtime.NumGoroutine(),
		Uptime: Duration(b.clock().Sub(b.started)), Version: Version}
	if rss, err := stats.RSS(filepath.Join(b.procRoot, "self", "status")); err == nil {
		s.RSS = rss
	}
	if rx, tx, err := stats.Network(filepath.Join(b.sysRoot, "class", "net")); err == nil {
		s.NetRx, s.NetTx = rx, tx
	}
	today, total, err := b.store.TrafficTotals(ctx, store.Day(b.clock()))
	if err != nil {
		return s, err
	}
	pending := b.traffic.Pending()
	s.APIToday, s.APITotal = today.Add(pending), total.Add(pending)
	if s.Vault, s.DataDir, err = b.diskSizes(ctx); err != nil {
		return s, fmt.Errorf("disk sizes: %w", err)
	}
	db := b.cfg.DBPath()
	s.DB = stats.FilesSize(db, db+"-wal", db+"-shm")
	if s.MediaRows, err = b.store.MediaCacheRows(ctx); err != nil {
		return s, err
	}
	b.mu.RLock()
	s.Commit, s.Notes, s.LastSync = b.sha, len(b.cat.Notes()), b.lastSync
	b.mu.RUnlock()
	s.CommitTime = b.repo.CommitTime(ctx, s.Commit)
	return s, nil
}

// NoteStat is a note and how often it was opened.
type NoteStat struct {
	ID    string `json:"id"`
	Title string `json:"title"` // "" if the note left the vault
	Path  string `json:"path,omitempty"`
	Opens int    `json:"opens"`
}

// AdminStats is the «Статистика» screen.
type AdminStats struct {
	Reports       store.SuggestionCounts `json:"reports"`
	ReportsWeek   store.SuggestionCounts `json:"reports_week"`
	TopWeek       []NoteStat             `json:"top_week"`
	TopAll        []NoteStat             `json:"top_all"`
	Broadcasts    int                    `json:"broadcasts"`
	LastBroadcast *store.Broadcast       `json:"last_broadcast,omitempty"`
}

// AdminStats counts reports, note opens and broadcasts.
func (b *Bot) AdminStats(ctx context.Context) (AdminStats, error) {
	var s AdminStats
	var err error
	week := b.clock().Add(-7 * 24 * time.Hour)
	if s.Reports, err = b.store.CountSuggestions(ctx, time.Time{}); err != nil {
		return s, err
	}
	if s.ReportsWeek, err = b.store.CountSuggestions(ctx, week); err != nil {
		return s, err
	}
	for _, t := range []struct {
		since string
		out   *[]NoteStat
	}{{store.Day(week), &s.TopWeek}, {"", &s.TopAll}} {
		top, err := b.store.TopNotes(ctx, t.since, 10)
		if err != nil {
			return s, err
		}
		cat := b.catalog()
		for _, c := range top {
			ns := NoteStat{ID: c.NoteID, Opens: c.Opens}
			if n := cat.Note(c.NoteID); n != nil {
				ns.Title, ns.Path = n.Title, n.Path
			}
			*t.out = append(*t.out, ns)
		}
	}
	all, err := b.store.Broadcasts(ctx)
	if err != nil {
		return s, err
	}
	s.Broadcasts = len(all)
	if len(all) > 0 {
		s.LastBroadcast = &all[0]
	}
	return s, nil
}

// AdminUser is a user as /admin shows them.
type AdminUser struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"` // the display name
	Username  string    `json:"username,omitempty"`
	FirstName string    `json:"first_name"`
	Nickname  string    `json:"nickname,omitempty"`
	Rank      string    `json:"rank"`
	Accepted  int       `json:"accepted"`
	Open      int       `json:"open"`
	Rejected  int       `json:"rejected"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	HideTop   bool      `json:"hide_top"`
	Blocked   bool      `json:"blocked"`
}

func (b *Bot) adminUser(u store.UserInfo) AdminUser {
	return AdminUser{ID: u.TgID, Name: u.DisplayName(), Username: u.Username, FirstName: u.FirstName,
		Nickname: u.Nickname, Rank: b.ladder().For(u.Accepted).Title, Accepted: u.Accepted, Open: u.Open,
		Rejected: u.Rejected, FirstSeen: u.FirstSeen, LastSeen: u.LastSeen, HideTop: u.HideTop, Blocked: u.Blocked}
}

// AdminUsers is a page of the user list, with the totals.
type AdminUsers struct {
	Counts store.UserCounts `json:"counts"`
	Query  string           `json:"query,omitempty"`
	Found  int              `json:"found"`
	Page   int              `json:"page"` // from 0
	Pages  int              `json:"pages"`
	Users  []AdminUser      `json:"users"`
}

// AdminUsers lists users matching query, the most recently active first,
// usersPerPage at a time. A page past the end shows the last one.
func (b *Bot) AdminUsers(ctx context.Context, query string, page int) (AdminUsers, error) {
	var r AdminUsers
	var err error
	if r.Counts, err = b.store.CountUsers(ctx, startOfDay(b.clock())); err != nil {
		return r, err
	}
	all, err := b.store.FindUsers(ctx, query)
	if err != nil {
		return r, err
	}
	r.Query, r.Found = query, len(all)
	r.Pages = max(1, (len(all)+usersPerPage-1)/usersPerPage)
	r.Page = min(max(page, 0), r.Pages-1)
	for _, u := range all[min(r.Page*usersPerPage, len(all)):min((r.Page+1)*usersPerPage, len(all))] {
		r.Users = append(r.Users, b.adminUser(u))
	}
	return r, nil
}

// ErrNoUser is returned for a user the bot does not know.
var ErrNoUser = errors.New("no such user")

// AdminUser finds a user by Telegram ID or @username.
func (b *Bot) AdminUser(ctx context.Context, ref string) (AdminUser, error) {
	u, err := b.findUser(ctx, ref)
	if err != nil {
		return AdminUser{}, err
	}
	info, err := b.store.UserInfo(ctx, u.TgID)
	if err != nil {
		return AdminUser{}, err
	}
	return b.adminUser(info), nil
}

func (b *Bot) findUser(ctx context.Context, ref string) (store.User, error) {
	ref = strings.TrimSpace(ref)
	var u store.User
	var err error
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		u, err = b.store.User(ctx, id)
	} else {
		u, err = b.store.UserByUsername(ctx, ref)
	}
	if errors.Is(err, store.ErrNotFound) {
		return u, ErrNoUser
	}
	return u, err
}

// ErrNoNickname is returned when resetting a nickname the user does not
// have.
var ErrNoNickname = errors.New("the user has no nickname")

// ResetNickname puts a user back on their Telegram name and tells them.
// It returns the nickname they had.
func (b *Bot) ResetNickname(ctx context.Context, ref string, by int64) (string, AdminUser, error) {
	u, err := b.findUser(ctx, ref)
	if err != nil {
		return "", AdminUser{}, err
	}
	if u.Nickname == "" {
		return "", AdminUser{}, ErrNoNickname
	}
	if err := b.store.SetNickname(ctx, u.TgID, "", ""); err != nil {
		return "", AdminUser{}, err
	}
	b.log.Info("nickname reset", "user", u.TgID, "by", by)
	b.send(ctx, u.TgID, b.t.NickResetNotice, nil)
	after, err := b.AdminUser(ctx, strconv.FormatInt(u.TgID, 10))
	return u.Nickname, after, err
}

// RecipientCount is how many users a broadcast would reach.
func (b *Bot) RecipientCount(ctx context.Context) (int, error) {
	ids, err := b.recipients(ctx)
	return len(ids), err
}

// BroadcastStarted describes a broadcast from the admin socket.
type BroadcastStarted struct {
	ID         int64 `json:"id"`
	Recipients int   `json:"recipients"`
}

// ErrBroadcastBusy is returned while another broadcast is being sent.
var ErrBroadcastBusy = errors.New("another broadcast is being sent; try again when it has finished")

// Broadcast sends a plain text, and optionally a picture, to every
// recipient in the background, as /broadcast does. The picture is
// uploaded once, to the cache chat or the first admin, and sent by its
// file_id. The message first goes to the first admin, as /broadcast's
// preview does: if Telegram refuses it, nobody else gets it either.
func (b *Bot) Broadcast(ctx context.Context, text string, photo []byte) (BroadcastStarted, error) {
	if strings.TrimSpace(text) == "" && len(photo) == 0 {
		return BroadcastStarted{}, errors.New("nothing to send")
	}
	if len(b.cfg.Admins) == 0 {
		return BroadcastStarted{}, errors.New("no admin to send the test copy to; set TERAKOYA_ADMIN_IDS")
	}
	if !b.broadcasting.TryLock() {
		return BroadcastStarted{}, ErrBroadcastBusy
	}
	locked := true
	defer func() {
		if locked {
			b.broadcasting.Unlock()
		}
	}()
	fileID := ""
	if len(photo) > 0 {
		var err error
		if fileID, err = b.uploadPhoto(ctx, photo); err != nil {
			return BroadcastStarted{}, fmt.Errorf("upload the picture: %w", err)
		}
	}
	bc := &store.Broadcast{Markdown: entitiesMarkdown(text, nil), Photo: fileID}
	if _, err := b.sendBroadcast(ctx, b.cfg.Admins[0], bc.Markdown, bc.Photo); err != nil {
		return BroadcastStarted{}, fmt.Errorf("the test copy to the first admin failed, so nothing was sent: %w", err)
	}
	// launchBroadcast releases the lock from here on.
	locked = false
	n, err := b.launchBroadcast(ctx, bc, 0)
	if err != nil {
		return BroadcastStarted{}, err
	}
	return BroadcastStarted{ID: bc.ID, Recipients: n}, nil
}

func (b *Bot) uploadPhoto(ctx context.Context, data []byte) (string, error) {
	chat := b.cfg.CacheChatID
	if chat == 0 {
		if len(b.cfg.Admins) == 0 {
			return "", errors.New("no cache chat and no admin to upload to")
		}
		chat = b.cfg.Admins[0]
	}
	msg, err := b.api.SendPhoto(ctx, &tg.SendPhotoParams{ChatID: chat,
		Photo: &models.InputFileUpload{Filename: "broadcast.jpg", Data: strings.NewReader(string(data))}})
	if err != nil {
		return "", err
	}
	if len(msg.Photo) == 0 {
		return "", errors.New("Telegram returned no photo")
	}
	if chat != b.cfg.CacheChatID {
		// Only the file_id was needed; the admin's chat stays clean.
		b.deleteMessage(ctx, chat, msg.ID)
	}
	return msg.Photo[len(msg.Photo)-1].FileID, nil
}
