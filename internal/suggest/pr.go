package suggest

import (
	"fmt"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
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

// Change summarizes a suggestion for the commit and the pull request.
type Change struct {
	Kind     string // "edit" or "new"
	Title    string
	RepoPath string // article path in the repository
	Images   []string
	BaseSHA  string
	ShortID  string
	Issues   []content.Issue
	By       Submitter
}

// Title of the pull request.
func (c Change) PRTitle() string {
	if c.Kind == "new" {
		return "Suggest new topic: " + c.Title
	}
	return "Suggest edit: " + c.Title
}

// CommitMessage for the suggestion commit.
func (c Change) CommitMessage() string {
	verb := "edit"
	if c.Kind == "new" {
		verb = "add"
	}
	return fmt.Sprintf("content: %s %s\n\nSuggested-by: %s (tg:%d)\n", verb, c.RepoPath, c.By.Handle(), c.By.TgID)
}

// PRBody lists who suggested the change, what it touches and what the
// linter found.
func (c Change) PRBody() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Suggested in Telegram by %s.\n\n", c.By.Handle())
	fmt.Fprintf(&b, "- Telegram user ID: `%d`\n", c.By.TgID)
	if c.By.Username != "" {
		fmt.Fprintf(&b, "- Username: @%s\n", c.By.Username)
	}
	if c.By.FirstName != "" {
		fmt.Fprintf(&b, "- Name: %s\n", c.By.FirstName)
	}
	fmt.Fprintf(&b, "- Article: `%s` (%s)\n", c.RepoPath, c.Kind)
	for _, img := range c.Images {
		fmt.Fprintf(&b, "- Image: `%s`\n", img)
	}
	fmt.Fprintf(&b, "- Based on: %s\n", c.BaseSHA)
	fmt.Fprintf(&b, "- Suggestion: `%s`\n", c.ShortID)
	if len(c.Issues) > 0 {
		b.WriteString("\n### Lint\n\n")
		for _, is := range c.Issues {
			fmt.Fprintf(&b, "- line %d: %s: %s\n", is.Line, is.Severity, is.Msg)
		}
	}
	return b.String()
}
