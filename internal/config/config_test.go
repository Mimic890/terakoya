package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileThenEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terakoya.yml")
	err := os.WriteFile(path, []byte("bot_token: from-file\nbot_username: \"@file_bot\"\ncache_chat_id: -100\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERAKOYA_BOT_TOKEN", "from-env")
	t.Setenv("TERAKOYA_RANKS", "a:0,b:1")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "from-env" || cfg.BotUsername != "file_bot" || cfg.CacheChatID != -100 {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.RepoBranch != "main" || cfg.ContentDir() != filepath.Join("content", "content") {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if len(cfg.Ladder) != 2 || cfg.Ladder.For(1).Title != "b" {
		t.Errorf("ladder = %+v", cfg.Ladder)
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

func TestExampleFile(t *testing.T) {
	cfg, err := Load("../../terakoya.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg.RepoDir != def.RepoDir || cfg.Ranks != def.Ranks || cfg.DataDir != def.DataDir {
		t.Errorf("example file differs from defaults: %+v", cfg)
	}
}
