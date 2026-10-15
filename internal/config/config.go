// Package config reads settings from an optional YAML file and environment
// variables. Environment variables take precedence over the file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

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
		RepoDir:       "./content",
		RepoBranch:    "main",
		ContentSubdir: "content",
		DataDir:       "./data",
		Ranks:         ranks.Default,
		LogLevel:      "info",
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

// Validate checks the options needed to run the bot.
func (c Config) Validate() error {
	if c.BotToken == "" {
		return fmt.Errorf("TERAKOYA_BOT_TOKEN is required")
	}
	return nil
}
