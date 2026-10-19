// Package config reads settings from an optional YAML file and environment
// variables. Environment variables take precedence over the file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds every option. The env tag names the environment variable,
// the yaml tag the key in the config file.
type Config struct {
	BotToken    string `yaml:"bot_token" env:"TERAKOYA_BOT_TOKEN"`
	BotUsername string `yaml:"bot_username" env:"TERAKOYA_BOT_USERNAME"`

	RepoDir    string `yaml:"repo_dir" env:"TERAKOYA_REPO_DIR"`
	RepoURL    string `yaml:"repo_url" env:"TERAKOYA_REPO_URL"`
	RepoBranch string `yaml:"repo_branch" env:"TERAKOYA_REPO_BRANCH"`

	// Git host for suggestions: "github" or "forgejo". Empty disables
	// suggestions, webhooks and sync.
	GitHost          string `yaml:"git_host" env:"TERAKOYA_GIT_HOST"`
	GitAPIURL        string `yaml:"git_api_url" env:"TERAKOYA_GIT_API_URL"`
	GitRepo          string `yaml:"git_repo" env:"TERAKOYA_GIT_REPO"`
	GitToken         string `yaml:"git_token" env:"TERAKOYA_GIT_TOKEN"`
	GitWebhookSecret string `yaml:"git_webhook_secret" env:"TERAKOYA_GIT_WEBHOOK_SECRET"`
	GitAuthorName    string `yaml:"git_author_name" env:"TERAKOYA_GIT_AUTHOR_NAME"`
	GitAuthorEmail   string `yaml:"git_author_email" env:"TERAKOYA_GIT_AUTHOR_EMAIL"`
	// AcceptedLabel marks an error report (an issue) as accepted when it is
	// closed.
	AcceptedLabel string `yaml:"accepted_label" env:"TERAKOYA_ACCEPTED_LABEL"`

	HTTPAddr     string        `yaml:"http_addr" env:"TERAKOYA_HTTP_ADDR"`
	SyncInterval time.Duration `yaml:"sync_interval" env:"TERAKOYA_SYNC_INTERVAL"`

	SuggestCooldown time.Duration `yaml:"suggest_cooldown" env:"TERAKOYA_SUGGEST_COOLDOWN"`
	SuggestMaxOpen  int64         `yaml:"suggest_max_open" env:"TERAKOYA_SUGGEST_MAX_OPEN"`
	ImageMaxBytes   int64         `yaml:"image_max_bytes" env:"TERAKOYA_IMAGE_MAX_BYTES"`

	// Resvg is the resvg binary used to rasterize SVG drawings; FontsDir
	// holds the DejaVu fonts it uses (found automatically when empty).
	Resvg    string `yaml:"resvg" env:"TERAKOYA_RESVG"`
	FontsDir string `yaml:"fonts_dir" env:"TERAKOYA_FONTS_DIR"`

	DataDir     string `yaml:"data_dir" env:"TERAKOYA_DATA_DIR"`
	CacheChatID int64  `yaml:"cache_chat_id" env:"TERAKOYA_CACHE_CHAT_ID"`
	LogLevel    string `yaml:"log_level" env:"TERAKOYA_LOG_LEVEL"`

	// AdminIDs lists the Telegram user IDs, comma-separated, of the admins,
	// who set up custom emoji and see the ranks.
	AdminIDs string `yaml:"admin_ids" env:"TERAKOYA_ADMIN_IDS"`

	// Admins is parsed from AdminIDs.
	Admins []int64 `yaml:"-"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		RepoDir:         "./content",
		RepoBranch:      "main",
		DataDir:         "./data",
		GitAuthorName:   "terakoya",
		GitAuthorEmail:  "terakoya@localhost",
		AcceptedLabel:   "accepted",
		HTTPAddr:        ":8080",
		SyncInterval:    5 * time.Minute,
		SuggestCooldown: time.Minute,
		SuggestMaxOpen:  5,
		ImageMaxBytes:   5 << 20,
		LogLevel:        "info",
		Resvg:           "resvg",
	}
}

// Load reads the config file at path (if not empty) and then the environment.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := applyEnv(&cfg, os.LookupEnv); err != nil {
		return cfg, err
	}
	var err error
	if cfg.Admins, err = parseIDs(cfg.AdminIDs); err != nil {
		return cfg, fmt.Errorf("TERAKOYA_ADMIN_IDS: %w", err)
	}
	cfg.BotUsername = strings.TrimPrefix(cfg.BotUsername, "@")
	return cfg, nil
}

func applyEnv(cfg *Config, lookup func(string) (string, bool)) error {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("env")
		if name == "" {
			continue
		}
		val, ok := lookup(name)
		if !ok || val == "" {
			continue
		}
		f := v.Field(i)
		if f.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(strings.TrimSpace(val))
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			f.SetInt(int64(d))
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString(val)
		case reflect.Int64:
			n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			f.SetInt(n)
		}
	}
	return nil
}

// parseIDs reads comma-separated Telegram user IDs.
func parseIDs(s string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%q is not a Telegram user ID", part)
		}
		out = append(out, id)
	}
	return out, nil
}

// IsAdmin reports whether the user is one of the admins.
func (c Config) IsAdmin(tgID int64) bool {
	return slices.Contains(c.Admins, tgID)
}

// DBPath is the SQLite database file.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "terakoya.db")
}

// Suggestions reports whether the git host is configured, which enables
// suggestions, git webhooks and content sync.
func (c Config) Suggestions() bool { return c.GitHost != "" }

// Validate checks the options needed to run the bot.
func (c Config) Validate() error {
	if c.BotToken == "" {
		return fmt.Errorf("TERAKOYA_BOT_TOKEN is required")
	}
	if !c.Suggestions() {
		return nil
	}
	switch c.GitHost {
	case "github", "forgejo":
	default:
		return fmt.Errorf("TERAKOYA_GIT_HOST must be github or forgejo")
	}
	var missing []string
	for name, v := range map[string]string{
		"TERAKOYA_REPO_URL":           c.RepoURL,
		"TERAKOYA_GIT_REPO":           c.GitRepo,
		"TERAKOYA_GIT_TOKEN":          c.GitToken,
		"TERAKOYA_GIT_WEBHOOK_SECRET": c.GitWebhookSecret,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if c.GitHost == "forgejo" && c.GitAPIURL == "" {
		missing = append(missing, "TERAKOYA_GIT_API_URL")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("with TERAKOYA_GIT_HOST set, these are required: %s", strings.Join(missing, ", "))
	}
	if strings.Count(c.GitRepo, "/") != 1 {
		return fmt.Errorf("TERAKOYA_GIT_REPO must be owner/name")
	}
	return nil
}

// SettingsPath is the settings file the bot keeps in its data directory.
func (c Config) SettingsPath() string {
	return filepath.Join(c.DataDir, "settings.yml")
}

// AdminSocketPath is the unix socket `terakoya admin` talks to the bot
// through.
func (c Config) AdminSocketPath() string {
	return filepath.Join(c.DataDir, "admin.sock")
}
