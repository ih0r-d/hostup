package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_@.-]+(?:\.service)?$`)

type Service struct {
	Name   string `json:"name"`
	Load   string `json:"load"`
	Active string `json:"active"`
	Sub    string `json:"sub"`
}

type CommandExecutor interface {
	Run(context.Context, string, ...string) ([]byte, error)
	Stream(context.Context, io.Writer, string, ...string) error
}

type execCommandExecutor struct{}

func (execCommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ensureSystemdOS(); err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

func (execCommandExecutor) Stream(ctx context.Context, dst io.Writer, name string, args ...string) error {
	if err := ensureSystemdOS(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = dst
	cmd.Stderr = dst
	return cmd.Run()
}

type ServiceManager struct{ executor CommandExecutor }

func NewServiceManager(executor CommandExecutor) *ServiceManager {
	if executor == nil {
		executor = execCommandExecutor{}
	}
	return &ServiceManager{executor: executor}
}

func (m *ServiceManager) List(ctx context.Context) ([]Service, error) {
	out, err := m.executor.Run(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager")
	if err != nil {
		return nil, commandError("list services", "systemctl", err)
	}
	var services []Service
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		services = append(services, Service{Name: fields[0], Load: fields[1], Active: fields[2], Sub: fields[3]})
	}
	return services, nil
}

func (m *ServiceManager) Status(ctx context.Context, name string) (string, error) {
	if err := validateServiceName(name); err != nil {
		return "", err
	}
	out, err := m.executor.Run(ctx, "systemctl", "status", "--no-pager", normalizeServiceName(name))
	if err != nil {
		return "", commandError("get status", "systemctl", err)
	}
	return string(out), nil
}

func (m *ServiceManager) Change(ctx context.Context, action, name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	if action != "start" && action != "stop" && action != "restart" && action != "enable" && action != "disable" {
		return fmt.Errorf("unsupported service action %q", action)
	}
	_, err := m.executor.Run(ctx, "systemctl", action, normalizeServiceName(name))
	if err != nil {
		return commandError(action+" service", "systemctl", err)
	}
	return nil
}

func (m *ServiceManager) Logs(ctx context.Context, dst io.Writer, name string, lines int, follow bool) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	if lines < 1 || lines > 10000 {
		return fmt.Errorf("lines must be between 1 and 10000")
	}
	args := []string{"--no-pager", "--unit", normalizeServiceName(name), "--lines", fmt.Sprint(lines)}
	if follow {
		args = append(args, "--follow")
	}
	if err := m.executor.Stream(ctx, dst, "journalctl", args...); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return commandError("read service logs", "journalctl", err)
	}
	return nil
}

func validateServiceName(name string) error {
	if len(name) == 0 || len(name) > 255 || !serviceNamePattern.MatchString(name) || strings.HasPrefix(name, "-") || strings.Contains(name, "..") || strings.HasSuffix(name, ".service.service") {
		return fmt.Errorf("invalid service name %q", name)
	}
	return nil
}

func ValidateServiceName(name string) error { return validateServiceName(name) }

func normalizeServiceName(name string) string {
	if strings.HasSuffix(name, ".service") {
		return name
	}
	return name + ".service"
}

func ensureSystemdOS() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("service management is unsupported on %s; Linux with systemd is required", runtime.GOOS)
	}
	return nil
}

func commandError(action, executable string, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		message := strings.TrimSpace(string(exitErr.Stderr))
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "not found"), strings.Contains(lower, "could not be found"), strings.Contains(lower, "not loaded"):
			return fmt.Errorf("%s: service not found: %s", action, message)
		case strings.Contains(lower, "access denied"), strings.Contains(lower, "authentication is required"), strings.Contains(lower, "not authorized"), strings.Contains(lower, "permission denied"):
			return fmt.Errorf("%s: insufficient permissions; run with the required privileges explicitly: %s", action, message)
		case strings.Contains(lower, "not booted with systemd"), strings.Contains(lower, "system has not been booted with systemd"), strings.Contains(lower, "failed to connect to bus"):
			return fmt.Errorf("%s: systemd is unavailable: %s", action, message)
		}
		if message != "" {
			return fmt.Errorf("%s failed: %s", action, message)
		}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s: %s is unavailable", action, executable)
	}
	return fmt.Errorf("%s failed: %w", action, err)
}
