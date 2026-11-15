package setupwizard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ih0r-d/hostup/src/internal/setup"
)

func TestSessionTagsResultsAndInterceptsQuit(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	s := NewSession(context.Background(), f.engine(), true)
	defer s.Close(nil)
	batch := s.Init()().(tea.BatchMsg)
	message := batch[0]()
	if _, ok := message.(sessionMsg); !ok {
		t.Fatal("async result not session-owned")
	}
	s.Update(message)
	if !s.model.loaded || len(f.calls) != 0 {
		t.Fatal("session not loaded or changed settings")
	}
	cmd := s.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if msg, ok := cmd().(FinishedMsg); !ok || msg.Session != s {
		t.Fatal("child quit escaped to parent")
	}
	other := NewSession(context.Background(), f.engine(), true)
	defer other.Close(nil)
	other.Update(message)
	if other.model.loaded {
		t.Fatal("old session response reached new session")
	}
}

func TestSessionCancellationPreservesPartialResults(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}, block: "timedatectl", entered: make(chan struct{})}
	m := readyModel(t, f)
	s := &Session{model: m}
	defer s.Close(nil)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	edit(m, 1, "Europe/Kyiv")
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	batch := s.command(press(m, tea.KeyEnter))().(tea.BatchMsg)
	done := make(chan struct{})
	go func() { batch[0](); close(done) }()
	<-f.entered
	if !s.Applying() {
		t.Fatal("session application state lost")
	}
	if cmd := s.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})); cmd != nil {
		t.Fatal("cancel quit before operation returned")
	}
	next := batch[1]
	for s.Applying() {
		if next == nil {
			t.Fatal("progress chain ended")
		}
		next = s.Update(next())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("application leaked")
	}
	s.Close(nil)
	if len(m.result.Applied) != 1 || m.result.Applied[0] != "hostname" || m.result.Failed != "timezone" || !errors.Is(m.err, context.Canceled) {
		t.Fatalf("partial result: %+v %v", m.result, m.err)
	}
	if summary := s.Summary(); !strings.Contains(summary, "Applied: hostname") || !strings.Contains(strings.ToLower(summary), "cancel") {
		t.Fatal(summary)
	}
}
