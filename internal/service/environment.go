package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Environment contains values passed to a managed service (e.g. tunnel
// credentials). Backends consume these differently: systemd and launchd load
// them from an EnvFile at runtime, and Windows SCM inlines them into the
// service registry.
type Environment map[string]string

// ParseEnvironment parses a systemd-compatible KEY=VALUE file. Blank lines and
// comments are ignored. Values may be single- or double-quoted.
func ParseEnvironment(r io.Reader) (Environment, error) {
	env := Environment{}
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || !validEnvKey(key) {
			return nil, fmt.Errorf("invalid environment entry on line %d", line)
		}
		env[key] = parseEnvValue(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read service environment: %w", err)
	}
	return env, nil
}

// LoadEnvironment loads a service env file. Missing files return an empty map.
func LoadEnvironment(path string) (Environment, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Environment{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open service environment: %w", err)
	}
	defer file.Close()
	return ParseEnvironment(file)
}

// WriteEnvironment writes an env file with private directory/file permissions.
func WriteEnvironment(path string, env Environment) error {
	if path == "" {
		return errors.New("service environment path is required")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace symlinked service environment file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect service environment: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create service environment directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-env-*")
	if err != nil {
		return fmt.Errorf("create service environment temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("set service environment permissions: %w", err)
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := env[key]
		if !validEnvKey(key) {
			tmp.Close()
			return fmt.Errorf("invalid service environment key %q", key)
		}
		if strings.ContainsAny(value, "\r\n") {
			tmp.Close()
			return fmt.Errorf("service environment value for %q contains a newline", key)
		}
		if _, err := fmt.Fprintf(tmp, "%s=%s\n", key, quoteEnvValue(value)); err != nil {
			tmp.Close()
			return fmt.Errorf("write service environment: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close service environment: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install service environment: %w", err)
	}
	return nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func parseEnvValue(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		if value[0] == '"' {
			if unquoted, err := strconv.Unquote(value); err == nil {
				return unquoted
			}
		}
		return value[1 : len(value)-1]
	}
	return value
}

func quoteEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool { return strings.ContainsRune(" \t#\"'\\", r) }) == -1 {
		return value
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
