package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcomontalbano/tailserve/internal/allowlist"
	"github.com/pelletier/go-toml/v2"
)

// AddRule appends rule to the config file at path, creating the file if needed.
// It reports false when an equal Rule already exists, updating its note if rule has one.
// Writing the file drops any comment except the header tailserve itself writes.
func AddRule(path string, rule allowlist.Rule) (bool, error) {
	file, err := readFile(path)
	if err != nil {
		return false, err
	}
	for index, entry := range file.Allow {
		existing, err := parseRule(entry)
		if err != nil {
			return false, fmt.Errorf("%s: [[allow]] number %d: %w", path, index+1, err)
		}
		if SameRule(existing, rule) {
			if rule.Note == "" || rule.Note == entry.Note {
				return false, nil
			}
			file.Allow[index].Note = rule.Note
			return false, writeFile(path, file)
		}
	}
	newEntry := entryOf(rule)
	if _, err := parseRule(newEntry); err != nil {
		return false, err
	}
	file.Allow = append(file.Allow, newEntry)
	return true, writeFile(path, file)
}

// RemoveRules deletes from the config file at path every Rule for which matches
// returns true, and returns the Rules it deleted.
func RemoveRules(path string, matches func(allowlist.Rule) bool) ([]allowlist.Rule, error) {
	file, err := readFile(path)
	if err != nil {
		return nil, err
	}
	var removedRules []allowlist.Rule
	keptEntries := file.Allow[:0]
	for index, entry := range file.Allow {
		rule, err := parseRule(entry)
		if err != nil {
			return nil, fmt.Errorf("%s: [[allow]] number %d: %w", path, index+1, err)
		}
		if matches(rule) {
			removedRules = append(removedRules, rule)
		} else {
			keptEntries = append(keptEntries, entry)
		}
	}
	if len(removedRules) == 0 {
		return nil, nil
	}
	file.Allow = keptEntries
	return removedRules, writeFile(path, file)
}

// SameRule reports whether a and b match the same Services: equal ports, and Process names equal ignoring case.
func SameRule(a, b allowlist.Rule) bool {
	if (a.Port == nil) != (b.Port == nil) {
		return false
	}
	if a.Port != nil && *a.Port != *b.Port {
		return false
	}
	return strings.EqualFold(a.Process, b.Process)
}

// entryOf turns rule into the TOML entry a person would write: a number for one port, a string for a range.
func entryOf(rule allowlist.Rule) ruleEntry {
	entry := ruleEntry{Process: rule.Process, Note: rule.Note}
	if rule.Port != nil {
		if rule.Port.IsSinglePort() {
			entry.Port = int64(rule.Port.First)
		} else {
			entry.Port = rule.Port.String()
		}
	}
	return entry
}

// readFile decodes the config file at path; a missing file is an empty one.
func readFile(path string) (configFile, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return configFile{}, nil
	}
	if err != nil {
		return configFile{}, fmt.Errorf("reading %s: %w", path, err)
	}
	file, err := decode(content)
	if err != nil {
		return configFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// writeFile replaces the config file at path in one step, so a running tailserve never reads it half written.
func writeFile(path string, file configFile) error {
	if _, err := file.config(); err != nil {
		return err
	}
	content, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encoding the config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(temporaryFile.Name())
	if _, err := temporaryFile.WriteString(header + "\n" + string(content)); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := temporaryFile.Chmod(0o644); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(temporaryFile.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
