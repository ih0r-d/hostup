package setup

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestApplyVerification(t *testing.T) {
	readErr := errors.New("state read unavailable")
	commandErr := errors.New("native failure")
	for _, tt := range []struct {
		name, skip, fail, drift    string
		readFailure                int
		sideEffect                 bool
		wantErr                    error
		applied, executed, pending []string
		failed                     string
		calls                      int
	}{
		{name: "applied and verified", applied: []string{"hostname", "timezone"}, executed: []string{"hostname", "timezone"}, calls: 2},
		{name: "successful command without side effect", skip: "hostnamectl", wantErr: ErrVerificationMismatch, executed: []string{"hostname"}, pending: []string{"timezone"}, failed: "hostname", calls: 1},
		{name: "verification unavailable", readFailure: 3, wantErr: ErrVerificationUnavailable, executed: []string{"hostname"}, pending: []string{"timezone"}, failed: "hostname", calls: 1},
		{name: "partial application", fail: "timedatectl", wantErr: commandErr, applied: []string{"hostname"}, executed: []string{"hostname"}, failed: "timezone", calls: 2},
		{name: "failure after side effect", fail: "hostnamectl", sideEffect: true, wantErr: commandErr, pending: []string{"timezone"}, failed: "hostname", calls: 1},
		{name: "timezone mismatch", skip: "timedatectl", wantErr: ErrVerificationMismatch, applied: []string{"hostname"}, executed: []string{"hostname", "timezone"}, failed: "timezone", calls: 2},
		{name: "earlier setting drifted", drift: "hostname", wantErr: ErrVerificationMismatch, applied: []string{"timezone"}, executed: []string{"hostname", "timezone"}, failed: "hostname", calls: 2},
		{name: "unavailable after partial verification", readFailure: 4, wantErr: ErrVerificationUnavailable, applied: []string{"hostname"}, executed: []string{"hostname", "timezone"}, failed: "timezone", calls: 2},
		{name: "pending setting drifted", drift: "timezone", wantErr: ErrStalePlan, applied: []string{"hostname"}, executed: []string{"hostname"}, pending: []string{"timezone"}, calls: 1},
		{name: "pending setting completed externally", drift: "completed", applied: []string{"hostname"}, executed: []string{"hostname"}, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			var events []Progress
			e := NewEngine(func(ctx context.Context) (Configuration, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("state read lacks deadline")
				}
				f.reads++
				if f.reads == tt.readFailure {
					return Configuration{}, readErr
				}
				return f.current, nil
			}, func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == tt.fail && !tt.sideEffect {
					f.calls = append(f.calls, commandCall{name, args})
					return nil, commandErr
				}
				if name == tt.skip {
					f.calls = append(f.calls, commandCall{name, args})
				} else {
					f.run(ctx, name, args...)
				}
				if name == "hostnamectl" && tt.drift == "timezone" {
					f.current.Timezone = "Europe/London"
				}
				if name == "hostnamectl" && tt.drift == "completed" {
					f.current.Timezone = "Europe/Kyiv"
				}
				if name == "timedatectl" && tt.drift == "hostname" {
					f.current.Hostname = "external-host"
				}
				if name == tt.fail {
					return nil, commandErr
				}
				return nil, nil
			})
			plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.ApplyWithProgress(context.Background(), plan, func(p Progress) {
				if p.State == Applied && f.reads < 3 {
					t.Fatal("success before read-back")
				}
				events = append(events, p)
			})
			if (tt.wantErr == nil && err != nil) || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("error %v want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(nonNil(r.Applied), nonNil(tt.applied)) || !reflect.DeepEqual(nonNil(r.Executed), nonNil(tt.executed)) || !reflect.DeepEqual(nonNil(r.Pending), nonNil(tt.pending)) || r.Failed != tt.failed || len(f.calls) != tt.calls {
				t.Fatalf("result=%+v calls=%v events=%v", r, f.calls, events)
			}
			if tt.readFailure != 0 && !errors.Is(err, readErr) {
				t.Fatal("lost read cause")
			}
			if errors.Is(err, ErrVerificationMismatch) {
				var v *VerificationError
				if !errors.As(err, &v) || v.Setting != tt.failed || v.Expected == v.Actual {
					t.Fatal("unstructured mismatch")
				}
			}
			if tt.skip == "hostnamectl" {
				for _, event := range events {
					if event.State == Applied {
						t.Fatal("false success event")
					}
				}
			}
			if tt.sideEffect && f.current.Hostname != "new-host" {
				t.Fatal("fixture failed to model a side effect")
			}
		})
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func TestUnknownVerificationAndRetry(t *testing.T) {
	for _, mode := range []string{"failed command after side effect", "failed verification", "cancelled verification"} {
		t.Run(mode, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := errors.New("failed after write")
			first := true
			e := NewEngine(func(context.Context) (Configuration, error) {
				f.reads++
				if first && f.reads == 3 {
					first = false
					if mode == "cancelled verification" {
						cancel()
						return Configuration{}, original
					}
					if mode == "failed verification" {
						return Configuration{}, original
					}
				}
				return f.current, nil
			}, func(ctx context.Context, name string, args ...string) ([]byte, error) {
				out, err := f.run(ctx, name, args...)
				if first && mode == "failed command after side effect" {
					first = false
					return nil, original
				}
				return out, err
			})
			plan, err := e.Plan(ctx, Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.Apply(ctx, plan)
			if !errors.Is(err, original) || len(r.Applied) != 0 || r.Failed != "hostname" || len(r.Pending) != 1 {
				t.Fatalf("first result %+v error %v", r, err)
			}
			if mode == "cancelled verification" && (!errors.Is(err, context.Canceled) || !errors.Is(err, ErrVerificationUnavailable)) {
				t.Fatal(err)
			}
			r, err = e.Apply(context.Background(), plan)
			if err != nil || len(f.calls) != 2 || len(r.Applied) != 1 || r.Applied[0] != "timezone" || len(r.Unchanged) != 1 || r.Unchanged[0] != "hostname" {
				t.Fatalf("retry %+v error %v calls %v", r, err, f.calls)
			}
		})
	}
}

func TestVerificationChecksRetainedSettings(t *testing.T) {
	f := &fixture{current: Configuration{"old-host", "UTC"}}
	e := NewEngine(f.read, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.run(ctx, name, args...)
		f.current.Timezone = "Europe/London"
		return nil, nil
	})
	plan, err := e.Plan(context.Background(), Desired{Hostname: pointer("new-host")})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Apply(context.Background(), plan)
	if !errors.Is(err, ErrVerificationMismatch) || r.Failed != "timezone" || len(r.Unchanged) != 0 || len(r.Applied) != 1 {
		t.Fatalf("retained drift %+v %v", r, err)
	}
}

func TestSetupErrorClassification(t *testing.T) {
	if !errors.Is(ValidateHostname(""), ErrInvalidConfiguration) || !errors.Is(ValidateTimezone("Unknown/Zone"), ErrInvalidConfiguration) {
		t.Fatal("validation not classifiable")
	}
	if !errors.Is(requireLinux("darwin"), ErrUnsupportedPlatform) {
		t.Fatal("unsupported platform not classifiable")
	}
	e := NewEngine(func(context.Context) (Configuration, error) { return Configuration{}, nil }, nil)
	if _, err := e.Plan(context.Background(), Desired{}); !errors.Is(err, ErrUnknownState) {
		t.Fatal(err)
	}
	f := &fixture{current: Configuration{"old-host", "UTC"}}
	e = NewEngine(f.read, func(context.Context, string, ...string) ([]byte, error) { return nil, os.ErrPermission })
	plan, _ := e.Plan(context.Background(), Desired{Hostname: pointer("new-host")})
	if r, err := e.Apply(context.Background(), plan); !errors.Is(err, os.ErrPermission) || len(r.Applied) != 0 || r.Failed != "hostname" {
		t.Fatalf("permission %+v %v", r, err)
	}
}

func TestExactTimezoneIdentifiers(t *testing.T) {
	f := &fixture{current: Configuration{"host", "Etc/UTC"}}
	plan, err := NewEngine(f.read, f.run).Plan(context.Background(), Desired{Timezone: pointer("UTC")})
	if err != nil || !plan.Timezone.Changed || plan.Timezone.Current != "Etc/UTC" || plan.Timezone.Desired != "UTC" || len(f.calls) != 0 {
		t.Fatalf("alias semantics changed: %+v %v", plan, err)
	}
}

func TestStateReadDefaultDeadlinePreservesCause(t *testing.T) {
	original := errors.New("inspection interrupted")
	e := NewEngine(func(ctx context.Context) (Configuration, error) { <-ctx.Done(); return Configuration{}, original }, nil)
	e.timeout = time.Millisecond
	if _, err := e.Plan(context.Background(), Desired{}); !errors.Is(err, original) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
