package system

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

type Platform struct {
	OS           string `json:"os"`
	Distribution string `json:"distribution"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Hostname     string `json:"hostname"`
}

func DetectPlatform() (Platform, error) {
	return detectPlatform(runtime.GOOS, runtime.GOARCH, os.ReadFile, os.Hostname)
}

func detectPlatform(goos, goarch string, readFile func(string) ([]byte, error), hostname func() (string, error)) (Platform, error) {
	if goos != "linux" {
		return Platform{}, fmt.Errorf("unsupported OS: %s", goos)
	}

	data, err := readFile("/etc/os-release")
	if errors.Is(err, os.ErrNotExist) {
		data, err = readFile("/usr/lib/os-release")
	}
	if err != nil {
		return Platform{}, fmt.Errorf("read os-release: %w", err)
	}

	values, err := ParseOSRelease(bytes.NewReader(data))
	if err != nil {
		return Platform{}, err
	}

	name, err := hostname()
	if err != nil {
		return Platform{}, fmt.Errorf("get hostname: %w", err)
	}

	return Platform{
		OS:           goos,
		Distribution: values["ID"],
		Version:      values["VERSION_ID"],
		Architecture: goarch,
		Hostname:     name,
	}, nil
}

func ParseOSRelease(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)

	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validOSReleaseKey(key) {
			return nil, fmt.Errorf("os-release line %d: invalid assignment", lineNumber)
		}

		value, err := parseOSReleaseValue(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("os-release line %d: %w", lineNumber, err)
		}
		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read os-release: %w", err)
	}

	if values["ID"] == "" {
		return nil, fmt.Errorf("os-release missing ID")
	}
	for _, char := range values["ID"] {
		if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && !strings.ContainsRune("._-", char) {
			return nil, fmt.Errorf("os-release invalid ID: %q", values["ID"])
		}
	}

	return values, nil
}

func validOSReleaseKey(key string) bool {
	if key == "" {
		return false
	}
	for i, char := range key {
		if char != '_' && !(char >= 'A' && char <= 'Z') && !(char >= 'a' && char <= 'z') && !(i > 0 && char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func parseOSReleaseValue(value string) (string, error) {
	var quote byte
	if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
		quote, value = value[0], value[1:]
	}

	var result strings.Builder
	for i := 0; i < len(value); i++ {
		char := value[i]
		if quote != 0 && char == quote {
			if i != len(value)-1 {
				return "", fmt.Errorf("unexpected text after closing quote")
			}
			return result.String(), nil
		}
		if char == '\\' && quote != '\'' {
			i++
			if i == len(value) {
				return "", fmt.Errorf("trailing escape")
			}
			if quote == '"' && !strings.ContainsRune("\"\\$`", rune(value[i])) {
				result.WriteByte('\\')
			}
			result.WriteByte(value[i])
			continue
		}
		if quote == 0 && strings.ContainsRune(" \t\"'", rune(char)) {
			return "", fmt.Errorf("unexpected whitespace or quote in unquoted value")
		}
		result.WriteByte(char)
	}
	if quote != 0 {
		return "", fmt.Errorf("unterminated quote")
	}
	return result.String(), nil
}
