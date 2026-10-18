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
}

// Handle is "@username", or the first name when there is no username.
func (s Submitter) Handle() string {
	if s.Username != "" {
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

// IssueBody describes the report for the reviewer.
func (r Report) IssueBody() string {
	var b strings.Builder
	what, where := "Error reported", "Article"
	if r.Grade {
		what, where = "Missing material reported", "Grade"
	}
	fmt.Fprintf(&b, "%s in Telegram by %s.\n\n", what, r.By.Handle())
	quote(&b, r.Text)
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Telegram user ID: `%d`\n", r.By.TgID)
	if r.By.Username != "" {
		fmt.Fprintf(&b, "- Username: @%s\n", r.By.Username)
	}
	if r.By.FirstName != "" {
		fmt.Fprintf(&b, "- Name: %s\n", r.By.FirstName)
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
			caption := p.Caption
			if caption == "" {
				caption = "no caption"
			}
			fmt.Fprintf(&b, "%d. ", i+1)
			if p.Path != "" {
				fmt.Fprintf(&b, "`%s` — ", p.Path)
			}
			fmt.Fprintf(&b, "%s\n", oneLine(caption))
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

// quote writes text as a Markdown blockquote.
func quote(b *strings.Builder, text string) {
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
	}
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
