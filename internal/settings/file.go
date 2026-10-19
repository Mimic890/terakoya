package settings

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"
)

// File is the settings file and the settings in use. They change through
// Update or when the file is edited; an invalid file leaves the settings
// in use as they were.
type File struct {
	path string
	log  *slog.Logger
	// OnChange, if set, is called with the new settings after each change.
	OnChange func(Settings)

	mu    sync.Mutex
	cur   Settings
	stamp stamp
}

type stamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) stamp {
	st, err := os.Stat(path)
	if err != nil {
		return stamp{}
	}
	return stamp{st.ModTime(), st.Size()}
}

// Open loads the settings file at path, creating it with defaults (and an
// entry for each of subjects) if it does not exist. A file that does not
// load leaves the defaults in use, and is not overwritten.
func Open(path string, subjects []string, log *slog.Logger) (*File, error) {
	f := &File{path: path, log: log}
	s, legacy, err := load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s = Default(subjects)
		if err := Save(path, s); err != nil {
			return nil, err
		}
		log.Info("settings created", "path", path)
	case err != nil:
		log.Error("settings file is invalid; using defaults until it is fixed", "err", err)
		s = Default(subjects)
	case legacy:
		if err := Save(path, s); err != nil {
			return nil, err
		}
		log.Info("settings: topic_list_style is now list_style; the file was updated", "path", path)
	}
	f.cur, f.stamp = s, stampOf(path)
	return f, nil
}

// Path is the settings file.
func (f *File) Path() string { return f.path }

// Get returns a copy of the settings in use.
func (f *File) Get() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur.Clone()
}

// Update changes the settings with edit, saves them and puts them in use.
// Invalid results are refused and nothing changes.
func (f *File) Update(edit func(*Settings)) error {
	f.mu.Lock()
	s := f.cur.Clone()
	edit(&s)
	if err := Save(f.path, s); err != nil {
		f.mu.Unlock()
		return err
	}
	f.cur, f.stamp = s, stampOf(f.path)
	f.mu.Unlock()
	f.changed(s)
	return nil
}

// Check rereads the file if it changed since it was last read or written.
func (f *File) Check() {
	f.mu.Lock()
	st := stampOf(f.path)
	if st == f.stamp {
		f.mu.Unlock()
		return
	}
	f.stamp = st
	s, err := Load(f.path)
	if err != nil {
		f.mu.Unlock()
		f.log.Error("settings file is invalid; keeping the previous settings", "err", err)
		return
	}
	f.cur = s
	f.mu.Unlock()
	f.log.Info("settings reloaded", "path", f.path)
	f.changed(s)
}

func (f *File) changed(s Settings) {
	if f.OnChange != nil {
		f.OnChange(s.Clone())
	}
}

// Watch checks the file every interval until ctx is done.
func (f *File) Watch(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.Check()
		}
	}
}
