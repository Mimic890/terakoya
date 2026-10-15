// Package config reads settings from an optional YAML file and environment
// variables. Environment variables take precedence over the file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Mimic890/terakoya/internal/ranks"
)

// Config holds every option. The env tag names the environment variable,
// the yaml tag the key in the config file.
type Config struct {
	BotToken    string `yaml:"bot_token" env:"TERAKOYA_BOT_TOKEN"`
	BotUsername string `yaml:"bot_username" env:"TERAKOYA_BOT_USERNAME"`

	RepoDir       string `yaml:"repo_dir" env:"TERAKOYA_REPO_DIR"`
	RepoURL       string `yaml:"repo_url" env:"TERAKOYA_REPO_URL"`
	RepoBranch    string `yaml:"repo_branch" env:"TERAKOYA_REPO_BRANCH"`
	ContentSubdir string `yaml:"content_subdir" env:"TERAKOYA_CONTENT_SUBDIR"`

	// Git host for suggestions: "github" or "forgejo". Empty disables
	// suggestions, webhooks and sync.
	GitHost          string `yaml:"git_host" env:"TERAKOYA_GIT_HOST"`
	GitAPIURL        string `yaml:"git_api_url" env:"TERAKOYA_GIT_API_URL"`
	GitRepo          string `yaml:"git_repo" env:"TERAKOYA_GIT_REPO"`
	GitToken         string `yaml:"git_token" env:"TERAKOYA_GIT_TOKEN"`
	GitWebhookSecret string `yaml:"git_webhook_secret" env:"TERAKOYA_GIT_WEBHOOK_SECRET"`
	GitAuthorName    string `yaml:"git_author_name" env:"TERAKOYA_GIT_AUTHOR_NAME"`
	GitAuthorEmail   string `yaml:"git_author_email" env:"TERAKOYA_GIT_AUTHOR_EMAIL"`

	HTTPAddr     string        `yaml:"http_addr" env:"TERAKOYA_HTTP_ADDR"`
	SyncInterval time.Duration `yaml:"sync_interval" env:"TERAKOYA_SYNC_INTERVAL"`

	SuggestCooldown time.Duration `yaml:"suggest_cooldown" env:"TERAKOYA_SUGGEST_COOLDOWN"`
	SuggestMaxOpen  int64         `yaml:"suggest_max_open" env:"TERAKOYA_SUGGEST_MAX_OPEN"`
	SuggestMaxBytes int64         `yaml:"suggest_max_bytes" env:"TERAKOYA_SUGGEST_MAX_BYTES"`
	ImageMaxBytes   int64         `yaml:"image_max_bytes" env:"TERAKOYA_IMAGE_MAX_BYTES"`

	DataDir     string `yaml:"data_dir" env:"TERAKOYA_DATA_DIR"`
	CacheChatID int64  `yaml:"cache_chat_id" env:"TERAKOYA_CACHE_CHAT_ID"`
	Ranks       string `yaml:"ranks" env:"TERAKOYA_RANKS"`
	LogLevel    string `yaml:"log_level" env:"TERAKOYA_LOG_LEVEL"`

	// Ladder is parsed from Ranks.
	Ladder ranks.Ladder `yaml:"-"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		RepoDir:         "./content",
		RepoBranch:      "main",
		ContentSubdir:   "content",
		DataDir:         "./data",
		GitAuthorName:   "terakoya",
		GitAuthorEmail:  "terakoya@localhost",
		HTTPAddr:        ":8080",
		SyncInterval:    5 * time.Minute,
		SuggestCooldown: time.Minute,
		SuggestMaxOpen:  5,
		SuggestMaxBytes: 256 << 10,
		ImageMaxBytes:   5 << 20,
		Ranks:           ranks.Default,
		LogLevel:        "info",
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
	ladder, err := ranks.Parse(cfg.Ranks)
	if err != nil {
		return cfg, fmt.Errorf("ranks: %w", err)
	}
	cfg.Ladder = ladder
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

// ContentDir is the catalog root inside the repository checkout.
func (c Config) ContentDir() string {
	return filepath.Join(c.RepoDir, c.ContentSubdir)
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
