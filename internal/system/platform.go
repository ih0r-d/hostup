package system

import (
	"bufio"
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
	if runtime.GOOS != "linux" {
		return Platform{}, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return Platform{}, fmt.Errorf("open os-release: %w", err)
	}
	defer file.Close()

	values, err := ParseOSRelease(file)
	if err != nil {
		return Platform{}, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return Platform{}, fmt.Errorf("get hostname: %w", err)
	}

	return Platform{
		OS:           runtime.GOOS,
		Distribution: values["ID"],
		Version:      values["VERSION_ID"],
		Architecture: runtime.GOARCH,
		Hostname:     hostname,
	}, nil
}

func ParseOSRelease(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		value = strings.Trim(strings.TrimSpace(value), `"'`)
		values[strings.TrimSpace(key)] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if values["ID"] == "" {
		return nil, fmt.Errorf("os-release missing ID")
	}

	return values, nil
}
