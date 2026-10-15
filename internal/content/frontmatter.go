package content

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the YAML header of an article.
type Frontmatter struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Order   *int     `yaml:"order"`
	Aliases []string `yaml:"aliases"`
}

var knownFrontmatterKeys = map[string]bool{
	"id": true, "title": true, "order": true, "aliases": true,
	// Common Obsidian keys that are harmless to keep.
	"tags": true, "cssclasses": true,
}

// SplitFrontmatter separates a leading "---" YAML block from the body.
// It returns the YAML source (without delimiters), the body, and the 1-based
// line where the body starts. ok is false if there is no frontmatter; an
// unterminated block is reported as an error.
func SplitFrontmatter(raw []byte) (yamlSrc, body string, bodyLine int, ok bool, err error) {
	src := string(bytes.TrimPrefix(raw, []byte("\ufeff")))
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return "", src, 1, false, nil
	}
	rest := src[4:]
	line := 2
	for off := 0; off <= len(rest); {
		end := strings.IndexByte(rest[off:], '\n')
		var l string
		if end < 0 {
			l = rest[off:]
		} else {
			l = rest[off : off+end]
		}
		if t := strings.TrimRight(l, " \t"); t == "---" || t == "..." {
			bodyStart := len(rest)
			if end >= 0 {
				bodyStart = off + end + 1
			}
			return rest[:off], rest[bodyStart:], line + 1, true, nil
		}
		if end < 0 {
			break
		}
		off += end + 1
		line++
	}
	return "", src, 1, false, fmt.Errorf("frontmatter is not terminated with ---")
}

// ParseFrontmatter decodes the YAML header and returns unknown keys.
func ParseFrontmatter(yamlSrc string) (Frontmatter, []string, error) {
	var fm Frontmatter
	if strings.TrimSpace(yamlSrc) == "" {
		return fm, nil, nil
	}
	if err := yaml.Unmarshal([]byte(yamlSrc), &fm); err != nil {
		return fm, nil, err
	}
	var all map[string]any
	if err := yaml.Unmarshal([]byte(yamlSrc), &all); err != nil {
		return fm, nil, err
	}
	var unknown []string
	for k := range all {
		if !knownFrontmatterKeys[k] {
			unknown = append(unknown, k)
		}
	}
	return fm, unknown, nil
}
