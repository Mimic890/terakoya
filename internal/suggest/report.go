// Package suggest describes what users report from the bot: the issue
// for an error report and the names of its pictures and branches.
package suggest

import (
	"fmt"
	"path"
	"strings"
)

// Submitter is the Telegram user behind a report.
type Submitter struct {
	TgID      int64
	Username  string
	FirstName string
	Nickname  string // chosen in the bot, or ""
}

// Handle is how the bot names the user: the nickname, else "@username",
// else the first name.
func (s Submitter) Handle() string {
	switch {
	case s.Nickname != "":
		return s.Nickname
	case s.Username != "":
		return "@" + s.Username
	}
	return s.FirstName
}

// Picture is an image the user sent with a report.
type Picture struct {
	// Path is the file in the repository; empty for issue attachments.
	Path    string
	Caption string
	// URL shows the picture in the issue.
	URL string
}

// Report is an error report on a note, or a report of something missing
// in a grade, for the issue and the commit of its pictures.
type Report struct {
	Title string // the note's title, or «Геометрия · 7 класс»
	// RepoPath is the note's path in the repository, or the grade folder.
	RepoPath string
	// Grade is set for a report about a grade rather than a note.
	Grade   bool
	BaseSHA string
	ShortID string
	By      Submitter
	// Text is what the user sent, verbatim.
	Text     string
	Pictures []Picture
	// ArticleURL and CurrentURL link to the note or grade at BaseSHA and
	// on the default branch.
	ArticleURL, CurrentURL string
	// AcceptLabel is the label that marks a report as accepted.
	AcceptLabel string
}

// IssueTitle is the title of the issue.
func (r Report) IssueTitle() string {
	if r.Grade {
		return "Missing material: " + r.Title
	}
	return "Error report: " + r.Title
}

// CommitMessage is the message of the commit that carries the pictures on
// hosts without issue attachments.
func (r Report) CommitMessage() string {
	return fmt.Sprintf("content: pictures for a report on %s\n\nSuggested-by: %s (tg:%d)\n", r.RepoPath, r.By.Handle(), r.By.TgID)
}

// IssueBody describes the report for the reviewer. Everything the user
// wrote, and their names, is shown as code: as Markdown it could mention
// people on the host (@user), link other issues (#12), load images from
// elsewhere in the reviewer's browser or pass for the bot's own lines.
func (r Report) IssueBody() string {
	var b strings.Builder
	what, where := "Error reported", "Article"
	if r.Grade {
		what, where = "Missing material reported", "Grade"
	}
	fmt.Fprintf(&b, "%s in Telegram by %s.\n\n", what, Inline(r.By.Handle()))
	b.WriteString(Fenced(r.Text))
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Telegram user ID: `%d`\n", r.By.TgID)
	if r.By.Username != "" {
		fmt.Fprintf(&b, "- Username: %s\n", Inline("@"+r.By.Username))
	}
	if r.By.FirstName != "" {
		fmt.Fprintf(&b, "- Name: %s\n", Inline(r.By.FirstName))
	}
	if r.By.Nickname != "" {
		fmt.Fprintf(&b, "- Nickname: %s\n", Inline(r.By.Nickname))
	}
	if r.ArticleURL != "" {
		fmt.Fprintf(&b, "- %s: [`%s`](%s) as the user saw it", where, r.RepoPath, r.ArticleURL)
		if r.CurrentURL != "" {
			fmt.Fprintf(&b, " · [current](%s)", r.CurrentURL)
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "- %s: `%s`\n", where, r.RepoPath)
	}
	fmt.Fprintf(&b, "- Based on: %s\n", r.BaseSHA)
	fmt.Fprintf(&b, "- Suggestion: `%s`\n", r.ShortID)
	if len(r.Pictures) > 0 {
		b.WriteString("\n### Pictures\n\n")
		for i, p := range r.Pictures {
			caption := "no caption"
			if c := oneLine(p.Caption); c != "" {
				caption = Inline(c)
			}
			fmt.Fprintf(&b, "%d. ", i+1)
			if p.Path != "" {
				fmt.Fprintf(&b, "`%s` — ", p.Path)
			}
			fmt.Fprintf(&b, "%s\n", caption)
			if p.URL != "" {
				fmt.Fprintf(&b, "\n   ![picture %d](%s)\n\n", i+1, p.URL)
			}
		}
	}
	if r.AcceptLabel != "" {
		fmt.Fprintf(&b, "\nFix it by hand, then close this issue with the `%s` label to count it as an accepted report. "+
			"Closing it without the label tells the user it was not accepted.\n", r.AcceptLabel)
	}
	return b.String()
}

// Fenced shows text as a code block, verbatim: nothing in it is Markdown.
// The fence is longer than any run of backticks in the text, so the text
// cannot close it.
func Fenced(text string) string {
	text = strings.TrimSpace(newlines.Replace(text))
	fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
	return fence + "text\n" + text + "\n" + fence + "\n"
}

// Inline shows one line of text as inline code, verbatim.
func Inline(s string) string {
	s = oneLine(s)
	if s == "" {
		return ""
	}
	ticks := strings.Repeat("`", longestRun(s, '`')+1)
	// A space on each side keeps a backtick at either end from joining
	// the delimiters; one is stripped on each side when it is rendered.
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return ticks + s + ticks
}

// newlines makes every line break a "\n"; Markdown ends lines at "\r"
// too, which would let a line escape the code block.
var newlines = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n")

func longestRun(s string, c rune) int {
	best, cur := 0, 0
	for _, r := range s {
		if r == c {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// LinkTarget writes a file name as the vault writes link targets: spaces
// and parentheses percent-encoded, Cyrillic as is.
func LinkTarget(name string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "(", "%28", ")", "%29", "#", "%23",
		"<", "%3C", ">", "%3E", "[", "%5B", "]", "%5D").Replace(name)
}

// RepoPath checks a vault-relative path from a session before it is
// committed or linked: it must not leave the vault.
func RepoPath(rel string) (string, error) {
	clean := path.Clean(rel)
	if clean != rel || path.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(rel, "\\") {
		return "", fmt.Errorf("bad path %q", rel)
	}
	return rel, nil
}
