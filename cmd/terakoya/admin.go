package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Mimic890/terakoya/internal/adminapi"
	"github.com/Mimic890/terakoya/internal/bot"
	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/stats"
)

const adminUsage = `usage: terakoya admin [--json] [--socket path] <command>

commands:
  status                              summary of the bot
  server                              memory, traffic, disk and vault
  stats                               reports, most opened notes, broadcasts
  users [--search q] [--page n]       list users (pages from 1)
  user <id|@username>                 one user
  nick-reset <id|@username>           reset a nickname and tell the user
  sync                                pull the vault now
  backup                              snapshot the database and settings now
  broadcast --text "…" [--photo path] [--yes]
                                      send a message to every user

The bot must be running; the socket is <data dir>/admin.sock.
`

// defaultSocket finds the socket the way the bot names it.
func defaultSocket() string {
	if p := os.Getenv("TERAKOYA_ADMIN_SOCKET"); p != "" {
		return p
	}
	cfg, err := config.Load(os.Getenv("TERAKOYA_CONFIG"))
	if err != nil {
		cfg = config.Default()
	}
	return cfg.AdminSocketPath()
}

// runAdmin runs `terakoya admin`; it returns the exit status.
func runAdmin(args []string, in io.Reader, out, errOut io.Writer, socket string) int {
	asJSON := false
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json" || a == "-json":
			asJSON = true
		case a == "--socket" || a == "-socket":
			if i+1 < len(args) {
				socket = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--socket="):
			socket = strings.TrimPrefix(a, "--socket=")
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		fmt.Fprint(errOut, adminUsage)
		return 2
	}
	cmd, cmdArgs := rest[0], rest[1:]
	c := adminapi.Dial(socket)
	ctx := context.Background()
	fail := func(err error) int {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	print := func(v any, text func(io.Writer)) int {
		if asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(v); err != nil {
				return fail(err)
			}
			return 0
		}
		text(out)
		return 0
	}
	switch cmd {
	case "status":
		s, err := c.Status(ctx)
		if err != nil {
			return fail(err)
		}
		return print(s, func(w io.Writer) { printStatus(w, s) })
	case "server":
		s, err := c.Server(ctx)
		if err != nil {
			return fail(err)
		}
		return print(s, func(w io.Writer) { printServer(w, s) })
	case "stats":
		s, err := c.Stats(ctx)
		if err != nil {
			return fail(err)
		}
		return print(s, func(w io.Writer) { printStats(w, s) })
	case "users":
		fs := flag.NewFlagSet("users", flag.ContinueOnError)
		fs.SetOutput(errOut)
		search := fs.String("search", "", "nickname, @username, first name or ID")
		page := fs.Int("page", 1, "page, from 1")
		if err := fs.Parse(cmdArgs); err != nil {
			return 2
		}
		u, err := c.Users(ctx, *search, *page-1)
		if err != nil {
			return fail(err)
		}
		return print(u, func(w io.Writer) { printUsers(w, u) })
	case "user", "nick-reset":
		if len(cmdArgs) != 1 {
			fmt.Fprintf(errOut, "usage: terakoya admin %s <id|@username>\n", cmd)
			return 2
		}
		if cmd == "user" {
			u, err := c.User(ctx, cmdArgs[0])
			if err != nil {
				return fail(err)
			}
			return print(u, func(w io.Writer) { printUser(w, u) })
		}
		r, err := c.NickReset(ctx, cmdArgs[0])
		if err != nil {
			return fail(err)
		}
		return print(r, func(w io.Writer) {
			fmt.Fprintf(w, "Nickname %q reset; the user is now %s and was told.\n", r.Old, r.User.Name)
		})
	case "sync":
		r, err := c.Sync(ctx)
		if err != nil {
			return fail(err)
		}
		code := print(r, func(w io.Writer) { printSync(w, r) })
		if r.Err != "" {
			return 1
		}
		return code
	case "backup":
		r, err := c.Backup(ctx)
		if err != nil {
			return fail(err)
		}
		code := print(r, func(w io.Writer) { printBackup(w, r) })
		if r.Err != "" {
			return 1
		}
		return code
	case "broadcast":
		return runBroadcast(ctx, c, cmdArgs, in, out, errOut, asJSON)
	}
	fmt.Fprintf(errOut, "unknown command %q\n\n%s", cmd, adminUsage)
	return 2
}

func runBroadcast(ctx context.Context, c *adminapi.Client, args []string, in io.Reader, out, errOut io.Writer, asJSON bool) int {
	fs := flag.NewFlagSet("broadcast", flag.ContinueOnError)
	fs.SetOutput(errOut)
	text := fs.String("text", "", "the message, plain text")
	photo := fs.String("photo", "", "a picture to send with it")
	yes := fs.Bool("yes", false, "send without asking")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*text) == "" && *photo == "" {
		fmt.Fprintln(errOut, "usage: terakoya admin broadcast --text \"…\" [--photo path] [--yes]")
		return 2
	}
	var data []byte
	if *photo != "" {
		var err error
		if data, err = os.ReadFile(*photo); err != nil {
			fmt.Fprintln(errOut, "error:", err)
			return 1
		}
	}
	n, err := c.Recipients(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	if !*yes {
		fmt.Fprintf(out, "Send to %d recipients? [y/N] ", n)
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "Not sent.")
			return 1
		}
	}
	r, err := c.Broadcast(ctx, *text, data)
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	if asJSON {
		json.NewEncoder(out).Encode(r)
		return 0
	}
	fmt.Fprintf(out, "The first admin got a test copy. Broadcast #%d started for %d recipients; it runs in the background.\n", r.ID, r.Recipients)
	return 0
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// since is a rounded age, or "unknown".
func since(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return age(time.Since(t)) + " ago"
}

func age(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

func when(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02 15:04")
}

func printStatus(w io.Writer, s bot.AdminStatus) {
	fmt.Fprintf(w, "Users:        %d (active in 24h: %d)\n", s.Users, s.Active24h)
	fmt.Fprintf(w, "Open reports: %d\n", s.OpenReports)
	fmt.Fprintf(w, "Vault:        %s, committed %s\n", short(s.Commit), since(s.CommitTime))
	fmt.Fprintf(w, "Uptime:       %s\n", age(time.Duration(s.Uptime)))
}

func printServer(w io.Writer, s bot.AdminServer) {
	B := stats.Bytes
	fmt.Fprintf(w, "Memory:      RSS %s, Go heap %s, %d goroutines\n", B(s.RSS), B(s.Heap), s.Goroutines)
	fmt.Fprintf(w, "Uptime:      %s, version %s\n", age(time.Duration(s.Uptime)), short(s.Version))
	fmt.Fprintf(w, "Network:     received %s, sent %s (container, since start)\n", B(s.NetRx), B(s.NetTx))
	fmt.Fprintf(w, "Telegram:    today %d calls, sent %s, received %s\n", s.APIToday.Calls, B(s.APIToday.Sent), B(s.APIToday.Received))
	fmt.Fprintf(w, "             total %d calls, sent %s, received %s\n", s.APITotal.Calls, B(s.APITotal.Sent), B(s.APITotal.Received))
	fmt.Fprintf(w, "Vault disk:  %s (notes %s, images %s, .git %s)\n", B(s.Vault.Total), B(s.Vault.Markdown), B(s.Vault.Images), B(s.Vault.Git))
	fmt.Fprintf(w, "Data:        %s, database %s, %d cached file_ids\n", B(s.DataDir), B(s.DB), s.MediaRows)
	fmt.Fprintf(w, "Vault:       commit %s (%s), %d notes\n", short(s.Commit), since(s.CommitTime), s.Notes)
	if r := s.LastSync; r != nil {
		outcome := "no changes"
		switch {
		case r.Err != "":
			outcome = "failed: " + r.Err
		case r.Changed():
			outcome = short(r.OldCommit) + " → " + short(r.NewCommit)
		}
		fmt.Fprintf(w, "Last sync:   %s, %s\n", when(r.At), outcome)
	} else {
		fmt.Fprintln(w, "Last sync:   none since start")
	}
	switch b := s.LastBackup; {
	case b == nil:
		fmt.Fprintln(w, "Backup:      none yet")
	case b.Err != "":
		fmt.Fprintf(w, "Backup:      %s, failed: %s\n", when(b.At), b.Err)
	default:
		fmt.Fprintf(w, "Backup:      %s, %s, %d snapshots\n", when(b.At), stats.Bytes(b.Size), b.Count)
	}
}

func printBackup(w io.Writer, r bot.BackupResult) {
	if r.Err != "" {
		fmt.Fprintf(w, "Backup failed: %s\n", r.Err)
		return
	}
	fmt.Fprintf(w, "Backup written and checked: %s (%s)\n", r.Path, stats.Bytes(r.Size))
	fmt.Fprintf(w, "Snapshots: %d kept, %d old ones removed\n", r.Count, r.Removed)
}

func printStats(w io.Writer, s bot.AdminStats) {
	fmt.Fprintf(w, "Reports, total:   open %d, accepted %d, rejected %d\n", s.Reports.Open, s.Reports.Accepted, s.Reports.Rejected)
	fmt.Fprintf(w, "Reports, 7 days:  open %d, accepted %d, rejected %d\n", s.ReportsWeek.Open, s.ReportsWeek.Accepted, s.ReportsWeek.Rejected)
	for _, top := range []struct {
		title string
		notes []bot.NoteStat
	}{{"Most opened, 7 days", s.TopWeek}, {"Most opened, all time", s.TopAll}} {
		fmt.Fprintf(w, "\n%s:\n", top.title)
		if len(top.notes) == 0 {
			fmt.Fprintln(w, "  none yet")
		}
		for i, n := range top.notes {
			title := n.Title
			if title == "" {
				title = n.ID + " (removed)"
			}
			fmt.Fprintf(w, "  %2d. %s — %d\n", i+1, title, n.Opens)
		}
	}
	fmt.Fprintf(w, "\nBroadcasts: %d\n", s.Broadcasts)
	if b := s.LastBroadcast; b != nil {
		fmt.Fprintf(w, "Last: %s, %d recipients, sent %d, blocked %d, failed %d\n", when(b.Created), b.Recipients, b.Sent, b.Blocked, b.Failed)
	}
}

func printUsers(w io.Writer, u bot.AdminUsers) {
	c := u.Counts
	fmt.Fprintf(w, "Users: %d, new today %d, new in 7 days %d, active 24h %d / 7 days %d, blocked the bot %d\n",
		c.All, c.NewToday, c.New7d, c.Active24h, c.Active7d, c.Blocked)
	if u.Query != "" {
		fmt.Fprintf(w, "Search %q: %d found\n", u.Query, u.Found)
	}
	fmt.Fprintf(w, "Page %d of %d\n\n", u.Page+1, u.Pages)
	for _, x := range u.Users {
		username := ""
		if x.Username != "" {
			username = "@" + x.Username
		}
		fmt.Fprintf(w, "%-12d %-24s %-20s last seen %s\n", x.ID, x.Name, username, when(x.LastSeen))
	}
}

func printUser(w io.Writer, u bot.AdminUser) {
	nick, username := u.Nickname, u.Username
	if nick == "" {
		nick = "none"
	}
	if username == "" {
		username = "none"
	} else {
		username = "@" + username
	}
	fmt.Fprintf(w, "%s\n", u.Name)
	fmt.Fprintf(w, "ID:         %d\nUsername:   %s\nFirst name: %s\nNickname:   %s\nRank:       %s\n", u.ID, username, u.FirstName, nick, u.Rank)
	fmt.Fprintf(w, "Reports:    accepted %d, open %d, rejected %d\n", u.Accepted, u.Open, u.Rejected)
	fmt.Fprintf(w, "First seen: %s\nLast seen:  %s\n", when(u.FirstSeen), when(u.LastSeen))
	fmt.Fprintf(w, "In the top: %s\nBlocked:    %s\n", map[bool]string{true: "hidden", false: "shown"}[u.HideTop],
		map[bool]string{true: "yes", false: "no"}[u.Blocked])
}

func printSync(w io.Writer, r bot.SyncResult) {
	switch {
	case r.Err != "":
		fmt.Fprintln(w, "Sync failed:", r.Err)
	case !r.Changed():
		fmt.Fprintf(w, "No changes, commit %s\n", short(r.NewCommit))
	default:
		fmt.Fprintf(w, "Updated %s → %s, notes %d → %d, %d errors, %d warnings\n",
			short(r.OldCommit), short(r.NewCommit), r.NotesBefore, r.NotesAfter, r.Errors, r.Warnings)
		for _, i := range r.Issues {
			fmt.Fprintln(w, "  "+i)
		}
	}
}
