package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDatabaseHint(t *testing.T) {
	hint := databaseHint(errors.New("migrate: unable to open database file (14)"), "/srv/data", 1000, 1000)
	if !strings.Contains(hint, "/srv/data") || !strings.Contains(hint, "chown -R 1000:1000") {
		t.Errorf("hint = %q", hint)
	}
	if hint := databaseHint(errors.New("database disk image is malformed"), "/srv/data", 1000, 1000); hint != "" {
		t.Errorf("hint for another error = %q", hint)
	}
}
