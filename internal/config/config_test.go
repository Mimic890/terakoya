package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileThenEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terakoya.yml")
	err := os.WriteFile(path, []byte("bot_token: from-file\nbot_username: \"@file_bot\"\ncache_chat_id: -100\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERAKOYA_BOT_TOKEN", "from-env")
	t.Setenv("TERAKOYA_RESVG", "") // empty variables are ignored
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "from-env" || cfg.BotUsername != "file_bot" || cfg.CacheChatID != -100 {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.RepoBranch != "main" || cfg.RepoDir != "./content" || cfg.Resvg != "resvg" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestErrors(t *testing.T) {
	t.Setenv("TERAKOYA_CACHE_CHAT_ID", "nope")
	if _, err := Load(""); err == nil {
		t.Error("bad integer accepted")
	}
	t.Setenv("TERAKOYA_CACHE_CHAT_ID", "")
	if _, err := Load(""); err != nil {
		t.Errorf("empty variable should be ignored: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bad.yml")
	os.WriteFile(path, []byte("unknown_key: 1\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("unknown key accepted")
	}
}

func TestAdminIDs(t *testing.T) {
	t.Setenv("TERAKOYA_ADMIN_IDS", " 12, 34 ,")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Admins) != 2 || !cfg.IsAdmin(34) || cfg.IsAdmin(56) {
		t.Errorf("admins = %v", cfg.Admins)
	}
	for _, bad := range []string{"12,abc", "-5", "@admin"} {
		t.Setenv("TERAKOYA_ADMIN_IDS", bad)
		if _, err := Load(""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExampleFile(t *testing.T) {
	cfg, err := Load("../../terakoya.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg.RepoDir != def.RepoDir || cfg.DataDir != def.DataDir {
		t.Errorf("example file differs from defaults: %+v", cfg)
	}
}

func TestDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terakoya.yml")
	if err := os.WriteFile(path, []byte("sync_interval: 90s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERAKOYA_SUGGEST_COOLDOWN", "2m")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncInterval != 90*time.Second || cfg.SuggestCooldown != 2*time.Minute {
		t.Errorf("durations = %v, %v", cfg.SyncInterval, cfg.SuggestCooldown)
	}
	t.Setenv("TERAKOYA_SUGGEST_COOLDOWN", "soon")
	if _, err := Load(""); err == nil {
		t.Error("bad duration accepted")
	}
}

func TestValidateGitHost(t *testing.T) {
	cfg := Default()
	cfg.BotToken = "x"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("suggestions off: %v", err)
	}
	cfg.GitHost = "forgejo"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "TERAKOYA_GIT_API_URL") || !strings.Contains(err.Error(), "TERAKOYA_GIT_TOKEN") {
		t.Fatalf("err = %v", err)
	}
	cfg.RepoURL, cfg.GitRepo, cfg.GitToken, cfg.GitWebhookSecret, cfg.GitAPIURL = "u", "o/r", "t", "s", "https://git.example"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.GitHost = "gitlab"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown host accepted")
	}
}
