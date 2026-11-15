package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func requireLinux(goos string) error {
	if goos != "linux" {
		return fmt.Errorf("setup is unsupported on %s; Linux is required: %w", goos, ErrUnsupportedPlatform)
	}
	return nil
}

func readLinux(ctx context.Context, run Runner) (Configuration, error) {
	if err := requireLinux(runtime.GOOS); err != nil {
		return Configuration{}, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return Configuration{}, fmt.Errorf("read hostname: %w", err)
	}
	timezone, err := readTimezone(ctx, "/etc/localtime", "/usr/share/zoneinfo", run)
	if err != nil {
		return Configuration{}, err
	}
	return Configuration{Hostname: hostname, Timezone: timezone}, nil
}

// Prefer the configured zoneinfo link. A copied/custom localtime file has no
// reliable zone name; ask timedatectl rather than trusting a stale /etc/timezone.
func readTimezone(ctx context.Context, localtime, zoneDir string, run Runner) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if target, err := os.Readlink(localtime); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(localtime), target)
		}
		name, err := filepath.Rel(zoneDir, target)
		if err == nil && ValidateTimezone(filepath.ToSlash(name)) == nil {
			// A dangling link does not establish the system's current timezone.
			if info, err := os.Stat(localtime); err == nil && info.Mode().IsRegular() {
				return filepath.ToSlash(name), nil
			}
		}
	}
	out, err := run(ctx, "timedatectl", "--no-ask-password", "show", "--property=Timezone", "--value")
	if err != nil {
		return "", fmt.Errorf("read timezone: %w", err)
	}
	name := strings.TrimSpace(string(out))
	if err := ValidateTimezone(name); err != nil {
		return "", fmt.Errorf("read timezone: %w", err)
	}
	return name, nil
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// Also bound waiting on inherited stdout/stderr pipes after a process exits.
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// Preserve the CLI's existing error text and cause while allowing UIs to detect
// permission failures with errors.Is(err, os.ErrPermission).
type permissionError struct{ cause error }

func (e permissionError) Error() string        { return e.cause.Error() }
func (e permissionError) Unwrap() error        { return e.cause }
func (e permissionError) Is(target error) bool { return target == os.ErrPermission }

func (e *Engine) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := e.run(ctx, name, args...)
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s: %w", name, errors.Join(err, ctx.Err()))
	}
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	message := ""
	if errors.As(err, &exitErr) {
		message = strings.TrimSpace(string(exitErr.Stderr))
	}
	detail := ""
	if message != "" {
		detail = ": " + message
	}
	lower := strings.ToLower(message)
	if errors.Is(err, os.ErrPermission) || strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "permission denied") || strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "authentication is required") || strings.Contains(lower, "authentication required") {
		return out, fmt.Errorf("%s: insufficient permissions; run with the required privileges explicitly%s: %w", name, detail, permissionError{err})
	}
	if errors.Is(err, exec.ErrNotFound) {
		return out, fmt.Errorf("%s is unavailable; Linux with the native systemd tools is required: %w", name, err)
	}
	return out, fmt.Errorf("%s failed%s: %w", name, detail, err)
}
