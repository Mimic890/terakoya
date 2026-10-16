package suggest

import (
	"fmt"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/plain"
)

// Suggestion kinds.
const (
	KindEdit   = "edit"
	KindNew    = "new"
	KindReport = "report"
)

// Submitter is the Telegram user behind a suggestion.
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

// Picture is an image the user sent, to be placed by the reviewer.
type Picture struct {
	// Path is the file in the repository; empty for issue attachments.
	Path    string
	Caption string
	// URL shows the picture in the pull request or issue.
	URL string
}

// Change summarizes a suggestion for the commit and the pull request or
// issue.
type Change struct {
	Kind     string
	Title    string
	RepoPath string // article path in the repository
	BaseSHA  string
	ShortID  string
	By       Submitter
	// Section is the part of the article that was edited; empty for the
	// whole article.
	Section string
	// Text is what the user sent, verbatim.
	Text string
	// Merge describes how an edit was merged into the article.
	Merge    *plain.Result
	Pictures []Picture
	Issues   []content.Issue
	// ArticleURL and CurrentURL link to the article at BaseSHA and on the
	// default branch (reports).
	ArticleURL, CurrentURL string
	// AcceptLabel is the label that marks a report as accepted.
	AcceptLabel string
}

// PRTitle is the title of the pull request or issue.
func (c Change) PRTitle() string {
	switch c.Kind {
	case KindNew:
		return "Suggest new topic: " + c.Title
	case KindReport:
		return "Error report: " + c.Title
	}
	return "Suggest edit: " + c.Title
}

// CommitMessage for the suggestion commit.
func (c Change) CommitMessage() string {
	verb := "edit"
	switch c.Kind {
	case KindNew:
		verb = "add"
	case KindReport:
		return fmt.Sprintf("content: pictures for an error report on %s\n\nSuggested-by: %s (tg:%d)\n", c.RepoPath, c.By.Handle(), c.By.TgID)
	}
	return fmt.Sprintf("content: %s %s\n\nSuggested-by: %s (tg:%d)\n", verb, c.RepoPath, c.By.Handle(), c.By.TgID)
}

func (c Change) who(b *strings.Builder, what string) {
	fmt.Fprintf(b, "%s in Telegram by %s.\n\n", what, c.By.Handle())
}

func (c Change) facts(b *strings.Builder) {
	fmt.Fprintf(b, "- Telegram user ID: `%d`\n", c.By.TgID)
	if c.By.Username != "" {
		fmt.Fprintf(b, "- Username: @%s\n", c.By.Username)
	}
	if c.By.FirstName != "" {
		fmt.Fprintf(b, "- Name: %s\n", c.By.FirstName)
	}
}

// PRBody describes an edit or a new topic for the reviewer: who sent it,
// what to check after the plain-text merge, the pictures to place, the
// text as sent and the lint findings.
func (c Change) PRBody() string {
	var b strings.Builder
	if c.Kind == KindNew {
		c.who(&b, "New topic suggested")
	} else {
		c.who(&b, "Edit suggested")
	}
	c.facts(&b)
	fmt.Fprintf(&b, "- Article: `%s`\n", c.RepoPath)
	if c.Section != "" {
		fmt.Fprintf(&b, "- Section: %s\n", c.Section)
	}
	fmt.Fprintf(&b, "- Based on: %s\n", c.BaseSHA)
	fmt.Fprintf(&b, "- Suggestion: `%s`\n", c.ShortID)

	if c.Kind == KindNew {
		b.WriteString("\n### To check\n\n")
		b.WriteString("- The text was typed as plain text: add formatting, formulas and [[links]].\n")
	}
	if m := c.Merge; m != nil {
		b.WriteString("\n### To check\n\n")
		switch m.Mode {
		case plain.Replaced:
			b.WriteString("- **The whole section was replaced** with the user's text.\n")
		case plain.Partial:
			b.WriteString("- The user sent only part of the section.\n")
		}
		for _, n := range m.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		if m.Shrunk {
			b.WriteString("- The text is much shorter than the section; the user confirmed sending it anyway.\n")
		}
		if len(m.Changes) > 0 {
			fmt.Fprintf(&b, "- %d changed place(s) below came in as plain text: check formatting, formulas and links.\n", len(m.Changes))
			for i, ch := range m.Changes {
				fmt.Fprintf(&b, "\n**%d.**", i+1)
				switch {
				case ch.Before == "":
					b.WriteString(" Added:\n")
					quote(&b, ch.After)
				case ch.After == "":
					b.WriteString(" Removed:\n")
					quote(&b, ch.Before)
				default:
					b.WriteString(" Was:\n")
					quote(&b, ch.Before)
					b.WriteString("\nNow:\n")
					quote(&b, ch.After)
				}
			}
		}
	}
	c.pictures(&b)
	if c.Text != "" {
		b.WriteString("\n### Text as sent\n\n<details><summary>Show</summary>\n\n")
		fenced(&b, c.Text)
		b.WriteString("\n</details>\n")
	}
	if len(c.Issues) > 0 {
		b.WriteString("\n### Lint\n\n")
		for _, is := range c.Issues {
			fmt.Fprintf(&b, "- line %d: %s: %s\n", is.Line, is.Severity, is.Msg)
		}
	}
	return b.String()
}

// IssueBody describes an error report.
func (c Change) IssueBody() string {
	var b strings.Builder
	c.who(&b, "Error reported")
	quote(&b, c.Text)
	b.WriteString("\n")
	c.facts(&b)
	if c.ArticleURL != "" {
		fmt.Fprintf(&b, "- Article: [`%s`](%s) as the user saw it", c.RepoPath, c.ArticleURL)
		if c.CurrentURL != "" {
			fmt.Fprintf(&b, " · [current](%s)", c.CurrentURL)
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "- Article: `%s`\n", c.RepoPath)
	}
	fmt.Fprintf(&b, "- Based on: %s\n", c.BaseSHA)
	fmt.Fprintf(&b, "- Suggestion: `%s`\n", c.ShortID)
	c.pictures(&b)
	if c.AcceptLabel != "" {
		fmt.Fprintf(&b, "\nFix it by hand, then close this issue with the `%s` label to count it as an accepted suggestion. "+
			"Closing it without the label tells the user it was not accepted.\n", c.AcceptLabel)
	}
	return b.String()
}

func (c Change) pictures(b *strings.Builder) {
	if len(c.Pictures) == 0 {
		return
	}
	b.WriteString("\n### Pictures\n\n")
	if c.Kind != KindReport {
		b.WriteString("Not referenced from the article yet: place them during review.\n\n")
	}
	for i, p := range c.Pictures {
		caption := p.Caption
		if caption == "" {
			caption = "no caption"
		}
		fmt.Fprintf(b, "%d. ", i+1)
		if p.Path != "" {
			fmt.Fprintf(b, "`%s` — ", p.Path)
		}
		fmt.Fprintf(b, "%s\n", oneLine(caption))
		if p.URL != "" {
			fmt.Fprintf(b, "\n   ![picture %d](%s)\n\n", i+1, p.URL)
		}
	}
}

// quote writes text as a Markdown blockquote.
func quote(b *strings.Builder, text string) {
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		b.WriteString(strings.TrimRight("> "+l, " ") + "\n")
	}
}

// fenced writes text in a code fence longer than any backtick run in it.
func fenced(b *strings.Builder, text string) {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	fmt.Fprintf(b, "%stext\n%s\n%s\n", fence, strings.TrimRight(text, "\n"), fence)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
