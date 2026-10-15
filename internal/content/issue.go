package content

import "fmt"

// Severity of a content issue.
type Severity int

const (
	Warning Severity = iota
	Error
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// Issue is a problem found in the content, reported by the loader, the
// converter or the linter.
type Issue struct {
	// Path is relative to the catalog root, slash-separated.
	Path     string
	Line     int
	Severity Severity
	Msg      string
}

func (i Issue) String() string {
	if i.Line > 0 {
		return fmt.Sprintf("%s:%d: %s: %s", i.Path, i.Line, i.Severity, i.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", i.Path, i.Severity, i.Msg)
}

// HasErrors reports whether any issue is an error.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == Error {
			return true
		}
	}
	return false
}
