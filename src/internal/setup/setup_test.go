package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pointer(value string) *string { return &value }

func TestValidation(t *testing.T) {
	for _, group := range []struct {
		name     string
		validate func(string) error
		valid    []string
		invalid  []string
	}{
		{"hostname", ValidateHostname,
			[]string{"pi", "pi-01", "host.example", "123", strings.Repeat("a", 63), strings.Repeat("a", 62) + ".b"},
			[]string{"", "Uppercase", "-host", "host-", "host..example", "host.example.", "bad_name", "a.-b", "a.b-", "a b", "хост", "host;id", strings.Repeat("a", 64), strings.Repeat("a", 63) + ".b"}},
		{"timezone", ValidateTimezone,
			[]string{"UTC", "Europe/Kyiv", "America/New_York", "Etc/GMT+1"},
			[]string{"", "Local", "Unknown/Zone", " UTC", "UTC\n", "/etc/localtime", "../UTC", "Europe//Kyiv", "Europe/Kyiv/", "UTC;id", "--help", "+03:00"}},
	} {
		for _, valid := range []bool{true, false} {
			values := group.valid
			if !valid {
				values = group.invalid
			}
			for _, value := range values {
				t.Run(group.name+"/"+value, func(t *testing.T) {
					if err := group.validate(value); (err == nil) != valid {
						t.Fatalf("validate(%q) = %v; valid=%t", value, err, valid)
					}
				})
			}
		}
	}
}

type commandCall struct {
	name string
	args []string
}

type fixture struct {
	current Configuration
	reads   int
	calls   []commandCall
	failAt  int
	err     error
}

func (f *fixture) read(context.Context) (Configuration, error) {
	f.reads++
	return f.current, nil
}

func (f *fixture) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, commandCall{name, append([]string(nil), args...)})
	if len(f.calls) == f.failAt {
		return nil, f.err
	}
	switch name {
	case "hostnamectl":
		f.current.Hostname = args[2]
	case "timedatectl":
		f.current.Timezone = args[2]
	}
	return nil, nil
}

func TestPlan(t *testing.T) {
	for _, tt := range []struct {
		name    string
		desired Desired
		want    Configuration
	}{
		{"omitted", Desired{}, Configuration{"old-host", "UTC"}},
		{"unchanged", Desired{pointer("old-host"), pointer("UTC")}, Configuration{"old-host", "UTC"}},
		{"hostname only", Desired{Hostname: pointer("new-host")}, Configuration{"new-host", "UTC"}},
		{"timezone only", Desired{Timezone: pointer("Europe/Kyiv")}, Configuration{"old-host", "Europe/Kyiv"}},
		{"both", Desired{pointer("new-host"), pointer("Europe/Kyiv")}, Configuration{"new-host", "Europe/Kyiv"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			plan, err := NewEngine(f.read, f.run).Plan(context.Background(), tt.desired)
			want := Plan{change("old-host", tt.want.Hostname), change("UTC", tt.want.Timezone)}
			if err != nil || plan != want || plan.HasChanges() != (tt.want != f.current) {
				t.Fatalf("Plan = %+v, %v; want %+v", plan, err, want)
			}
			if f.reads != 1 || len(f.calls) != 0 {
				t.Fatalf("plan changed system: reads=%d calls=%v", f.reads, f.calls)
			}
		})
	}
}

func TestInvalidDesiredBeforeRead(t *testing.T) {
	for _, desired := range []Desired{
		{Hostname: pointer("")}, {Timezone: pointer("")},
		{Hostname: pointer("-bad")}, {pointer("new-host"), pointer("Unknown/Zone")},
	} {
		f := &fixture{current: Configuration{"old-host", "UTC"}}
		if _, err := NewEngine(f.read, f.run).Plan(context.Background(), desired); err == nil || f.reads != 0 || len(f.calls) != 0 {
			t.Fatalf("invalid desired accepted or read system: %+v", desired)
		}
	}
}

func TestApplyAndRepeat(t *testing.T) {
	for _, tt := range []struct {
		name    string
		desired Desired
		calls   []commandCall
	}{
		{"no changes", Desired{}, nil},
		{"hostname", Desired{Hostname: pointer("new-host")}, []commandCall{{"hostnamectl", []string{"--no-ask-password", "set-hostname", "new-host"}}}},
		{"timezone", Desired{Timezone: pointer("Europe/Kyiv")}, []commandCall{{"timedatectl", []string{"--no-ask-password", "set-timezone", "Europe/Kyiv"}}}},
		{"both", Desired{pointer("new-host"), pointer("Europe/Kyiv")}, []commandCall{
			{"hostnamectl", []string{"--no-ask-password", "set-hostname", "new-host"}},
			{"timedatectl", []string{"--no-ask-password", "set-timezone", "Europe/Kyiv"}},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			e := NewEngine(f.read, f.run)
			plan, err := e.Plan(context.Background(), tt.desired)
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Apply(context.Background(), plan)
			if err != nil || !reflect.DeepEqual(f.calls, tt.calls) || len(result.Applied) != len(tt.calls) || len(result.Unchanged) != 2-len(tt.calls) || len(result.Pending) != 0 || result.Failed != "" {
				t.Fatalf("Apply = %+v, %v; calls=%v", result, err, f.calls)
			}
			// Reusing an approved plan and generating a fresh plan must both be safe.
			result, err = e.Apply(context.Background(), plan)
			if err != nil || len(result.Applied) != 0 || len(result.Unchanged) != 2 || !reflect.DeepEqual(f.calls, tt.calls) {
				t.Fatalf("repeat Apply = %+v, %v; calls=%v", result, err, f.calls)
			}
			fresh, err := e.Plan(context.Background(), tt.desired)
			if err != nil || fresh.HasChanges() {
				t.Fatalf("fresh plan = %+v, %v", fresh, err)
			}
		})
	}
}

func TestApplyFailures(t *testing.T) {
	wantErr := errors.New("command failed")
	for _, tt := range []struct {
		name, failed string
		failAt       int
		applied      []string
		pending      []string
	}{
		{"first operation", "hostname", 1, []string{}, []string{"timezone"}},
		{"partial application", "timezone", 2, []string{"hostname"}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}, failAt: tt.failAt, err: wantErr}
			e := NewEngine(f.read, f.run)
			plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			result, err := e.Apply(context.Background(), plan)
			if !errors.Is(err, wantErr) || result.Failed != tt.failed || !reflect.DeepEqual(result.Applied, tt.applied) || !reflect.DeepEqual(result.Pending, tt.pending) || len(f.calls) != tt.failAt {
				t.Fatalf("Apply = %+v, %v; calls=%v", result, err, f.calls)
			}
		})
	}
}

func TestRetryPartialApplication(t *testing.T) {
	for _, failedAfterChange := range []bool{false, true} {
		f := &fixture{current: Configuration{"old-host", "UTC"}}
		failed := false
		wantErr := errors.New("timezone operation failed")
		e := NewEngine(f.read, func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "timedatectl" && !failed {
				failed = true
				f.calls = append(f.calls, commandCall{name, args})
				if failedAfterChange {
					f.current.Timezone = args[2]
				}
				return nil, wantErr
			}
			return f.run(ctx, name, args...)
		})
		plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
		if err != nil {
			t.Fatal(err)
		}
		result, err := e.Apply(context.Background(), plan)
		if !errors.Is(err, wantErr) || result.Failed != "timezone" || !reflect.DeepEqual(result.Applied, []string{"hostname"}) {
			t.Fatalf("first Apply=%+v, %v", result, err)
		}
		result, err = e.Apply(context.Background(), plan)
		wantApplied := 1
		if failedAfterChange {
			wantApplied = 0
		}
		if err != nil || len(result.Applied) != wantApplied || len(f.calls) != 2+wantApplied {
			t.Fatalf("retry afterChange=%t: result=%+v, err=%v calls=%v", failedAfterChange, result, err, f.calls)
		}
	}
}

func TestApplyPreflight(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*Plan, *fixture)
		want   string
	}{
		{"invalid hostname", func(p *Plan, _ *fixture) { p.Hostname.Desired = "-bad" }, "invalid hostname"},
		{"invalid timezone before hostname", func(p *Plan, _ *fixture) { p.Timezone.Desired = "Unknown/Zone" }, "invalid timezone"},
		{"inconsistent plan", func(p *Plan, _ *fixture) { p.Hostname.Changed = false }, "inconsistent"},
		{"stale hostname", func(_ *Plan, f *fixture) { f.current.Hostname = "another-host" }, "changed since planning"},
		{"stale timezone before hostname", func(_ *Plan, f *fixture) { f.current.Timezone = "Europe/London" }, "changed since planning"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			e := NewEngine(f.read, f.run)
			plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(&plan, f)
			if _, err := e.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), tt.want) || len(f.calls) != 0 {
				t.Fatalf("preflight error=%v calls=%v", err, f.calls)
			}
		})
	}
}

func TestReadFailures(t *testing.T) {
	wantErr := errors.New("read failed")
	for _, tt := range []struct {
		name    string
		current Configuration
		err     error
	}{
		{"error", Configuration{}, wantErr},
		{"missing hostname", Configuration{Timezone: "UTC"}, nil},
		{"missing timezone", Configuration{Hostname: "old-host"}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			e := NewEngine(func(context.Context) (Configuration, error) { return tt.current, tt.err }, func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil })
			if _, err := e.Plan(context.Background(), Desired{}); err == nil || (tt.err != nil && !errors.Is(err, wantErr)) {
				t.Fatalf("Plan error=%v", err)
			}
			if _, err := e.Apply(context.Background(), Plan{}); err == nil || calls != 0 {
				t.Fatalf("Apply error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestCancellationAndTimeouts(t *testing.T) {
	for _, mode := range []string{"cancelled before read", "read deadline", "command timeout", "parent deadline", "partial cancellation"} {
		t.Run(mode, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			e := NewEngine(f.read, f.run)
			plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := context.DeadlineExceeded
			switch mode {
			case "cancelled before read":
				cancel()
				wantErr = context.Canceled
				reads := f.reads
				if _, err := e.Plan(ctx, Desired{}); !errors.Is(err, wantErr) || f.reads != reads {
					t.Fatalf("cancelled Plan: %v reads=%d", err, f.reads)
				}
			case "read deadline":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
				e.readCurrent = func(ctx context.Context) (Configuration, error) { <-ctx.Done(); return Configuration{}, ctx.Err() }
			case "command timeout", "parent deadline":
				if mode == "command timeout" {
					e.timeout = 10 * time.Millisecond
				} else {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
					defer stop()
				}
				e.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					f.calls = append(f.calls, commandCall{name, args})
					<-ctx.Done()
					return nil, ctx.Err()
				}
			case "partial cancellation":
				wantErr = context.Canceled
				e.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name == "timedatectl" {
						cancel()
						return nil, ctx.Err()
					}
					return f.run(ctx, name, args...)
				}
			}
			result, err := e.Apply(ctx, plan)
			if !errors.Is(err, wantErr) {
				t.Fatalf("Apply = %+v, %v; want %v", result, err, wantErr)
			}
			if mode == "partial cancellation" {
				if !reflect.DeepEqual(result.Applied, []string{"hostname"}) || result.Failed != "timezone" {
					t.Fatalf("partial result=%+v", result)
				}
			} else if len(result.Applied) != 0 {
				t.Fatalf("unexpected application: %+v", result)
			}
		})
	}
}

func TestProgress(t *testing.T) {
	for _, tt := range []struct {
		name    string
		desired Desired
		failAt  int
		want    []Progress
	}{
		{"unchanged", Desired{}, 0, []Progress{{"hostname", Unchanged}, {"timezone", Unchanged}}},
		{"success", Desired{pointer("new-host"), pointer("Europe/Kyiv")}, 0,
			[]Progress{{"hostname", Running}, {"hostname", Applied}, {"timezone", Running}, {"timezone", Applied}}},
		{"failure", Desired{pointer("new-host"), pointer("Europe/Kyiv")}, 1,
			[]Progress{{"hostname", Running}, {"hostname", Failed}}},
		{"partial", Desired{pointer("new-host"), pointer("Europe/Kyiv")}, 2,
			[]Progress{{"hostname", Running}, {"hostname", Applied}, {"timezone", Running}, {"timezone", Failed}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}, failAt: tt.failAt, err: errors.New("failed")}
			var events []Progress
			e := NewEngine(f.read, func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if len(events) == 0 || events[len(events)-1].State != Running {
					t.Fatal("operation reported completion before executing")
				}
				return f.run(ctx, name, args...)
			})
			plan, err := e.Plan(context.Background(), tt.desired)
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.ApplyWithProgress(context.Background(), plan, func(event Progress) { events = append(events, event) })
			if !reflect.DeepEqual(events, tt.want) || (err != nil) != (tt.failAt != 0) {
				t.Fatalf("events=%v error=%v", events, err)
			}
		})
	}
}

func TestPreflightPreservesUnattemptedChanges(t *testing.T) {
	for _, mode := range []string{"cancelled", "stale", "read failure"} {
		t.Run(mode, func(t *testing.T) {
			f := &fixture{current: Configuration{"old-host", "UTC"}}
			e := NewEngine(f.read, f.run)
			plan, err := e.Plan(context.Background(), Desired{pointer("new-host"), pointer("Europe/Kyiv")})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "cancelled":
				cancel()
			case "stale":
				f.current.Timezone = "Europe/London"
			case "read failure":
				e.readCurrent = func(context.Context) (Configuration, error) { return Configuration{}, errors.New("read failed") }
			}
			result, err := e.ApplyWithProgress(ctx, plan, func(Progress) { t.Fatal("preflight emitted operation progress") })
			if err == nil || len(f.calls) != 0 || len(result.Applied) != 0 || len(result.Unchanged) != 0 || !reflect.DeepEqual(result.Pending, []string{"hostname", "timezone"}) {
				t.Fatalf("result=%+v error=%v calls=%v", result, err, f.calls)
			}
		})
	}
}
