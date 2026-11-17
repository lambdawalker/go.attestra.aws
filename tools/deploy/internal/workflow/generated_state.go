package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func legacyStatePaths(root string) ([]string, error) {
	paths, e := statePaths(root, "bootstrap.local.json*", "bootstrap.*.local.json*", "teardown.*.local.json*", "index-publication.*.local.json*", "teardown-history")
	if e != nil {
		return nil, e
	}
	old, e := statePaths(filepath.Join(root, ".attestra"), "*.local.json*", "teardown-history", "*.lock")
	return append(paths, old...), e
}
func prepareGeneratedState(root string) error {
	if data, e := os.ReadFile(filepath.Join(root, ".deploy", "records-migrated.json")); e == nil {
		var marker struct {
			Version     int
			Destination string
		}
		if json.Unmarshal(data, &marker) != nil || marker.Version != 1 || marker.Destination != stateDirectory(root) {
			return errors.New("corrupt or mismatched record migration marker; inspect recovery histories before continuing")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}

	paths, e := legacyStatePaths(root)
	if e != nil {
		return e
	}
	if len(paths) > 0 {
		return errors.New("legacy deployment records found; stop other operators, back up .attestra/ and run deploy -migrate-records -project <name>; originals and vaults will be retained")
	}
	return os.MkdirAll(stateDirectory(root), 0700)
}

// Explicit, resumable local copy. No resources, identities or secret scopes change.
// Originals remain recovery copies; a conflict never overwrites either history.
func migrateGeneratedState(root string) error {
	release, e := acquireOperatorLock(root)
	if e != nil {
		return e
	}
	defer release()
	dir := stateDirectory(root)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	lock := filepath.Join(dir, "records-migration.lock")
	f, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	f.Close()
	defer os.Remove(lock)
	paths, e := legacyStatePaths(root)
	if e != nil {
		return e
	}
	active, e := statePaths(dir, "*.lock")
	if e != nil {
		return e
	}
	for _, path := range active {
		if path != lock && path != filepath.Join(dir, "operator.lock") {
			return fmt.Errorf("active/stale destination operation lock: %s", path)
		}
	}
	for _, path := range paths {
		if strings.HasSuffix(path, ".lock") {
			return fmt.Errorf("inspect active/stale operation lock before migration: %s", path)
		}
	}
	for _, path := range paths {
		if e = copyRecovery(path, filepath.Join(dir, filepath.Base(path))); e != nil {
			return e
		}
	}
	return atomicPrivateJSON(filepath.Join(root, ".deploy", "records-migrated.json"), map[string]any{"Version": 1, "Source": "legacy checkout records", "Destination": dir})
}
func copyRecovery(source, target string) error {
	info, e := os.Lstat(source)
	if e != nil {
		return e
	}
	if info.IsDir() {
		entries, e := os.ReadDir(source)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(target, 0700); e != nil {
			return e
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".lock") {
				return fmt.Errorf("active recovery lock: %s", entry.Name())
			}
			if e = copyRecovery(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); e != nil {
				return e
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported recovery entry %s", source)
	}
	data, e := os.ReadFile(source)
	if e != nil {
		return e
	}
	old, e := os.ReadFile(target)
	if e == nil {
		if !bytes.Equal(old, data) {
			return fmt.Errorf("conflicting recovery histories: %s and %s", source, target)
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(target), "recovery-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if _, e = os.Lstat(target); !os.IsNotExist(e) {
		return fmt.Errorf("recovery destination appeared or is inaccessible: %s", target)
	}
	return os.Rename(f.Name(), target)
}

func statePaths(dir string, patterns ...string) ([]string, error) {
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var paths []string
	for _, entry := range entries {
		for _, pattern := range patterns {
			match, e := filepath.Match(pattern, entry.Name())
			if e != nil {
				return nil, e
			}
			if match {
				paths = append(paths, filepath.Join(dir, entry.Name()))
				break
			}
		}
	}
	return paths, nil
}
