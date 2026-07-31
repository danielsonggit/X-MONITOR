package grok

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const runMarker = ".x-monitor-grok-run-v1"

var runDirectoryPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{32}$`)

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func privateWrite(path string, content []byte) error {
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+uuid.NewString()+".tmp")
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	removeTemporary = false
	return os.Chmod(path, 0o600)
}

func privateWriteJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return privateWrite(path, content)
}

func copyValidJSON(source, destination string) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4<<20 {
		return nil
	}
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	var value any
	if json.Unmarshal(content, &value) != nil {
		return nil
	}
	return privateWrite(destination, content)
}

func persistValidJSON(source, destination string) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4<<20 {
		return nil
	}
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	var value any
	if json.Unmarshal(content, &value) != nil {
		return nil
	}
	return privateWrite(destination, content)
}

func insideGitWorktree(path string) bool {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	for {
		if _, err := os.Lstat(filepath.Join(resolved, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return false
		}
		resolved = parent
	}
}

func cleanupExpiredRuns(root string, retentionDays int, now time.Time) error {
	if retentionDays < 0 {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || !runDirectoryPattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		marker, err := os.ReadFile(filepath.Join(path, runMarker))
		if err != nil || strings.TrimSpace(string(marker)) != "x-monitor Grok run v1" {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		expected, err := filepath.Abs(path)
		if err != nil || resolved != expected || filepath.Dir(resolved) != root {
			continue
		}
		if err := os.RemoveAll(resolved); err != nil {
			return fmt.Errorf("remove expired run %s: %w", entry.Name(), err)
		}
	}
	return nil
}
