package daemon

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bnkrr/rses/internal/session"
)

func TestRunDAGHonorsParallelLimit(t *testing.T) {
	s := &Server{}
	var running int32
	var maxRunning int32
	targets := []operationTarget{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		name := name
		targets = append(targets, operationTarget{
			ID:   testID(name),
			Name: name,
			Run: func(context.Context, eventEmitter) operationResult {
				current := atomic.AddInt32(&running, 1)
				for {
					max := atomic.LoadInt32(&maxRunning)
					if current <= max || atomic.CompareAndSwapInt32(&maxRunning, max, current) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return operationResult{Name: name, Result: "ok"}
			},
		})
	}

	summary, _ := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "status", 2, targets, false, emit)
	})

	if got := atomic.LoadInt32(&maxRunning); got > 2 {
		t.Fatalf("max running = %d, want <= 2", got)
	}
	if len(summary.Results) != len(targets) {
		t.Fatalf("results = %+v, want all targets", summary.Results)
	}
}

func TestRunDAGSkipsStartDependentsWhenDependencyFails(t *testing.T) {
	s := &Server{}
	targets := []operationTarget{
		{
			ID:   testID("db"),
			Name: "db",
			Run: func(context.Context, eventEmitter) operationResult {
				return operationResult{Name: "db", Result: "failed: boom", Failed: true, Error: "boom"}
			},
		},
		{
			Name: "api",
			ID:   testID("api"),
			Deps: []operationID{testID("db")},
			Run: func(context.Context, eventEmitter) operationResult {
				t.Fatal("dependent target should not run after dependency failure")
				return operationResult{}
			},
		},
		{
			ID:   testID("sidecar"),
			Name: "sidecar",
			Run: func(context.Context, eventEmitter) operationResult {
				return operationResult{Name: "sidecar", Result: "ok"}
			},
		},
	}

	summary, events := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "start", 4, targets, true, emit)
	})

	if summary.Results["db"] != "failed: boom" {
		t.Fatalf("db result = %q", summary.Results["db"])
	}
	if !strings.HasPrefix(summary.Results["api"], "skipped: dependency db failed") {
		t.Fatalf("api result = %q, want dependency skip", summary.Results["api"])
	}
	if summary.Results["sidecar"] != "ok" {
		t.Fatalf("sidecar result = %q, want ok", summary.Results["sidecar"])
	}
	if !hasEvent(events, "skipped", "api") {
		t.Fatalf("events = %+v, want api skipped event", events)
	}
}

func TestRunDAGStopBestEffortDoesNotSkipDependents(t *testing.T) {
	s := &Server{}
	targets := []operationTarget{
		{
			ID:   testID("app"),
			Name: "app",
			Run: func(context.Context, eventEmitter) operationResult {
				return operationResult{Name: "app", Result: "failed: stop failed", Failed: true, Error: "stop failed"}
			},
		},
		{
			Name: "dependency",
			ID:   testID("dependency"),
			Deps: []operationID{testID("app")},
			Run: func(context.Context, eventEmitter) operationResult {
				return operationResult{Name: "dependency", Result: "stopped"}
			},
		},
	}

	summary, _ := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "stop", 4, targets, false, emit)
	})

	if summary.Results["app"] != "failed: stop failed" {
		t.Fatalf("app result = %q", summary.Results["app"])
	}
	if summary.Results["dependency"] != "stopped" {
		t.Fatalf("dependency result = %q, want best-effort stop", summary.Results["dependency"])
	}
}

func TestRunDAGStatusEmitsStatusesAsTargetsFinish(t *testing.T) {
	s := &Server{}
	targets := []operationTarget{
		statusTarget("slow", 20*time.Millisecond),
		statusTarget("fast", 0),
	}

	summary, events := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "status", 2, targets, false, emit)
	})

	if len(summary.Statuses) != 2 {
		t.Fatalf("statuses = %+v, want both statuses", summary.Statuses)
	}
	statusEvents := eventNames(events, "status")
	if len(statusEvents) != 2 || statusEvents[0] != "fast" {
		t.Fatalf("status event order = %+v, want completion order with fast first", statusEvents)
	}
}

func TestRunDAGAllowsSameNameInDifferentScopes(t *testing.T) {
	s := &Server{}
	targets := []operationTarget{
		sameNameTarget(operationID{Scope: "scope-one", Window: "demo"}, "one"),
		sameNameTarget(operationID{Scope: "scope-two", Window: "demo"}, "two"),
	}

	summary, events := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "status", 2, targets, false, emit)
	})

	if len(eventNames(events, "status")) != 2 {
		t.Fatalf("events = %+v, want both same-name status events", events)
	}
	if len(summary.Statuses) != 2 {
		t.Fatalf("statuses = %+v, want both same-name statuses", summary.Statuses)
	}
	if len(summary.Results) != 2 {
		t.Fatalf("results = %+v, want both same-name results preserved by operation ID", summary.Results)
	}
}

func TestRunDAGRejectsDuplicateOperationID(t *testing.T) {
	s := &Server{}
	id := operationID{Scope: "scope", Window: "same"}
	ran := false
	targets := []operationTarget{
		{
			ID:   id,
			Name: "api/v1",
			Run: func(context.Context, eventEmitter) operationResult {
				ran = true
				return operationResult{Name: "api/v1", Result: "unexpected"}
			},
		},
		{
			ID:   id,
			Name: "api_v1",
			Run: func(context.Context, eventEmitter) operationResult {
				ran = true
				return operationResult{Name: "api_v1", Result: "unexpected"}
			},
		},
	}

	summary, events := collectOperation(t, func(ctx context.Context, emit eventEmitter) operationSummary {
		return s.runDAG(ctx, "status", 2, targets, false, emit)
	})

	if ran {
		t.Fatal("duplicate operation ID should fail before running targets")
	}
	if len(summary.Results) != 0 || len(summary.Statuses) != 0 {
		t.Fatalf("summary = %+v, want empty summary after duplicate operation ID", summary)
	}
	if got := firstFailedError(events); !strings.Contains(got, `both resolve to operation target "scope:same"`) {
		t.Fatalf("error = %q, events=%+v", got, events)
	}
}

func statusTarget(name string, delay time.Duration) operationTarget {
	return operationTarget{
		ID:   testID(name),
		Name: name,
		Run: func(_ context.Context, emit eventEmitter) operationResult {
			time.Sleep(delay)
			status := session.Status{Name: name, State: session.StateRunning}
			_ = emit(OperationEvent{Type: "status", Name: name, Status: &status})
			return operationResult{Name: name, Status: status}
		},
	}
}

func sameNameTarget(id operationID, result string) operationTarget {
	return operationTarget{
		ID:   id,
		Name: "demo",
		Run: func(_ context.Context, emit eventEmitter) operationResult {
			status := session.Status{Name: "demo", State: session.StateRunning, Summary: result}
			_ = emit(OperationEvent{Type: "status", Name: "demo", Status: &status})
			return operationResult{ID: id, Name: "demo", Result: result, Status: status}
		},
	}
}

func testID(name string) operationID {
	return operationID{Scope: "test", Window: name}
}

func hasEvent(events []OperationEvent, typ, name string) bool {
	for _, event := range events {
		if event.Type == typ && event.Name == name {
			return true
		}
	}
	return false
}

func eventNames(events []OperationEvent, typ string) []string {
	names := []string{}
	for _, event := range events {
		if event.Type == typ {
			names = append(names, event.Name)
		}
	}
	return names
}
