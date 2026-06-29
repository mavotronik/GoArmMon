package config

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

func Watch(ctx context.Context, path string, onChange func(*Config)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("watch directory: %w", err)
	}

	go func() {
		defer watcher.Close()

		var debounce <-chan time.Time
		var debounceTimer *time.Timer

		triggerReload := func() {
			cfg, err := Load(path)
			if err != nil {
				slog.Warn("config reload failed", "error", err)
				return
			}
			slog.Info("config reloaded", "path", path)
			onChange(cfg)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name != path {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.NewTimer(500 * time.Millisecond)
				debounce = debounceTimer.C
			case <-debounce:
				debounce = nil
				triggerReload()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("config watcher error", "error", err)
			}
		}
	}()

	return nil
}
