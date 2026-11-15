// Package setup plans and applies Linux configuration independently of any UI.
package setup

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Configuration is the observed hostname and named system timezone.
type Configuration struct {
	Hostname string `json:"hostname"`
	Timezone string `json:"timezone"`
}

// Desired distinguishes omitted settings (nil) from explicitly empty values.
type Desired struct {
	Hostname *string
	Timezone *string
}

// Change describes one setting, including settings retained without changes.
type Change struct {
	Current string `json:"current"`
	Desired string `json:"desired"`
	Changed bool   `json:"changed"`
}

// Plan is the snapshot a CLI or TUI presents for approval before Apply.
type Plan struct {
	Hostname Change `json:"hostname"`
	Timezone Change `json:"timezone"`
}

func (p Plan) HasChanges() bool { return p.Hostname.Changed || p.Timezone.Changed }

// Result records observations, not a transaction. Applied settings were read
// back at their desired values; Executed records successful commands, including
// any not subsequently verified. A failed command may have had side effects.
// Observations are snapshots: no lock prevents later external changes.
type Result struct {
	Applied   []string
	Executed  []string
	Unchanged []string
	Failed    string
	Pending   []string
}

// Unverified lists successful commands without a matching latest observation.
func (r Result) Unverified() []string {
	var fields []string
	for _, field := range r.Executed {
		if !slices.Contains(r.Applied, field) {
			fields = append(fields, field)
		}
	}
	return fields
}

// Progress reports operation boundaries synchronously, without command output.
// Applied is emitted only after execution and a successful state read-back.
type Progress struct {
	Setting string
	State   OperationState
}

type OperationState string

const (
	Running   OperationState = "running"
	Applied   OperationState = "applied"
	Unchanged OperationState = "unchanged"
	Failed    OperationState = "failed"
)

// Runner follows the existing system command executor's Run signature. It must
// honor ctx; adapters can pass an existing executor's Run method directly.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ReadCurrent must be read-only and honor ctx.
type ReadCurrent func(context.Context) (Configuration, error)

// Engine owns setup business logic; it neither prompts nor renders output.
type Engine struct {
	readCurrent ReadCurrent
	run         Runner
	timeout     time.Duration
}

// NewEngine accepts fixture/adaptor functions or nil for native Linux defaults.
// State reads and commands receive 30-second deadlines bounded by caller ctx.
func NewEngine(read ReadCurrent, run Runner) *Engine {
	if run == nil {
		run = runCommand
	}
	e := &Engine{run: run, timeout: 30 * time.Second}
	if read == nil {
		read = func(ctx context.Context) (Configuration, error) { return readLinux(ctx, e.command) }
	}
	e.readCurrent = read
	return e
}

// Plan validates all supplied values before reading the current configuration.
// It never executes a configuration-changing command.
func (e *Engine) Plan(ctx context.Context, desired Desired) (Plan, error) {
	if err := desired.validate(); err != nil {
		return Plan{}, err
	}
	current, err := e.current(ctx)
	if err != nil {
		return Plan{}, err
	}
	target := current
	if desired.Hostname != nil {
		target.Hostname = *desired.Hostname
	}
	if desired.Timezone != nil {
		target.Timezone = *desired.Timezone
	}
	return Plan{Hostname: change(current.Hostname, target.Hostname), Timezone: change(current.Timezone, target.Timezone)}, nil
}

func change(current, desired string) Change {
	return Change{Current: current, Desired: desired, Changed: current != desired}
}

// Apply executes an approved plan in hostname/timezone order, stopping at the
// first failure. It rereads the system: already applied values are skipped, and
// unrelated changes since planning require reviewing a new plan.
func (e *Engine) Apply(ctx context.Context, plan Plan) (Result, error) {
	return e.ApplyWithProgress(ctx, plan, nil)
}

// ApplyWithProgress has the same safety and result semantics as Apply. notify
// must return promptly; UIs should enqueue events rather than render here.
func (e *Engine) ApplyWithProgress(ctx context.Context, plan Plan, notify func(Progress)) (Result, error) {
	result := Result{Applied: []string{}, Unchanged: []string{}, Pending: []string{}}
	operations := []struct {
		field, command, action string
		change                 Change
		validate               func(string) error
	}{
		{"hostname", "hostnamectl", "set-hostname", plan.Hostname, ValidateHostname},
		{"timezone", "timedatectl", "set-timezone", plan.Timezone, ValidateTimezone},
	}
	// Retain unattempted changes even if preflight fails or is cancelled.
	for _, op := range operations {
		if op.change.Changed {
			result.Pending = append(result.Pending, op.field)
		}
	}
	// Plans are public values: validate them again before executing any changes.
	for _, op := range operations {
		if op.change.Changed != (op.change.Current != op.change.Desired) {
			return result, fmt.Errorf("%w: inconsistent %s plan", ErrInvalidConfiguration, op.field)
		}
		if op.change.Changed {
			if err := op.validate(op.change.Desired); err != nil {
				return result, err
			}
		}
	}
	current, err := e.current(ctx)
	if err != nil {
		return result, err
	}
	values := []string{current.Hostname, current.Timezone}
	for i, op := range operations {
		if values[i] != op.change.Current && values[i] != op.change.Desired {
			return result, fmt.Errorf("%w: %s changed since planning; review a new plan", ErrStalePlan, op.field)
		}
	}
	result.Pending = result.Pending[:0]
	for i, op := range operations {
		if values[i] == op.change.Desired {
			result.Unchanged = append(result.Unchanged, op.field)
			if notify != nil {
				notify(Progress{op.field, Unchanged})
			}
		} else {
			result.Pending = append(result.Pending, op.field)
		}
	}
	for i, op := range operations {
		if values[i] == op.change.Desired {
			// A previous verification may observe an externally completed change.
			if index := slices.Index(result.Pending, op.field); index >= 0 {
				result.Pending = slices.Delete(result.Pending, index, index+1)
				result.Unchanged = append(result.Unchanged, op.field)
				if notify != nil {
					notify(Progress{op.field, Unchanged})
				}
			}
			continue
		}
		if values[i] != op.change.Current {
			return result, fmt.Errorf("%w: %s changed since planning; review a new plan", ErrStalePlan, op.field)
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if notify != nil {
			notify(Progress{op.field, Running})
		}
		_, err := e.command(ctx, op.command, "--no-ask-password", op.action, op.change.Desired)
		result.Pending = result.Pending[1:]
		if err != nil {
			result.Failed = op.field
			if notify != nil {
				notify(Progress{op.field, Failed})
			}
			return result, fmt.Errorf("apply %s: %w", op.field, err)
		}
		result.Executed = append(result.Executed, op.field)
		observed, readErr := e.current(ctx)
		if readErr != nil {
			result.Failed = op.field
			if notify != nil {
				notify(Progress{op.field, Failed})
			}
			return result, &VerificationError{Setting: op.field, Expected: op.change.Desired, Cause: readErr}
		}
		values = []string{observed.Hostname, observed.Timezone}
		// Recheck every completed/retained setting in this snapshot. Pending
		// changes still have their old values and are checked when reached.
		result.Applied = result.Applied[:0]
		for j, checked := range operations {
			if values[j] == checked.change.Desired && slices.Contains(result.Executed, checked.field) {
				result.Applied = append(result.Applied, checked.field)
			}
			if values[j] != checked.change.Desired {
				if index := slices.Index(result.Unchanged, checked.field); index >= 0 {
					result.Unchanged = slices.Delete(result.Unchanged, index, index+1)
				}
			}
		}
		if notify != nil {
			if slices.Contains(result.Applied, op.field) {
				notify(Progress{op.field, Applied})
			}
		}
		for j, checked := range operations {
			if !slices.Contains(result.Pending, checked.field) && values[j] != checked.change.Desired {
				result.Failed = checked.field
				if notify != nil {
					notify(Progress{checked.field, Failed})
				}
				return result, &VerificationError{Setting: checked.field, Expected: checked.change.Desired, Actual: values[j]}
			}
		}
	}
	return result, nil
}

func (e *Engine) current(ctx context.Context) (Configuration, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	current, err := e.readCurrent(ctx)
	if ctx.Err() != nil {
		return Configuration{}, errors.Join(err, ctx.Err())
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("read current setup: %w", err)
	}
	if current.Hostname == "" || current.Timezone == "" {
		return Configuration{}, fmt.Errorf("read current setup: %w: hostname and timezone must be known", ErrUnknownState)
	}
	return current, nil
}

func (d Desired) validate() error {
	if d.Hostname != nil {
		if err := ValidateHostname(*d.Hostname); err != nil {
			return err
		}
	}
	if d.Timezone != nil {
		return ValidateTimezone(*d.Timezone)
	}
	return nil
}

// ValidateHostname accepts lowercase DNS labels within Linux's 64-byte limit.
func ValidateHostname(name string) error {
	invalid := func() error {
		return fmt.Errorf("%w: invalid hostname %q: use lowercase DNS labels, at most 64 bytes total", ErrInvalidConfiguration, name)
	}
	if len(name) == 0 || len(name) > 64 {
		return invalid()
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return invalid()
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return invalid()
			}
		}
	}
	return nil
}

// ValidateTimezone requires a named location from the available IANA database;
// Go's special Local value is process-dependent and is not a system timezone.
func ValidateTimezone(name string) error {
	if name == "" || name == "Local" {
		return fmt.Errorf("%w: invalid timezone %q: use an IANA timezone such as UTC or Europe/Kyiv", ErrInvalidConfiguration, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" {
			return fmt.Errorf("%w: invalid timezone %q", ErrInvalidConfiguration, name)
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') && !strings.ContainsRune("_+-", c) {
				return fmt.Errorf("%w: invalid timezone %q", ErrInvalidConfiguration, name)
			}
		}
	}
	if _, err := time.LoadLocation(name); err != nil {
		return fmt.Errorf("invalid timezone %q: %w", name, errors.Join(ErrInvalidConfiguration, err))
	}
	return nil
}
