package resource

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recorder struct {
	name   string
	calls  *[]string
	failAt string
}

func (r recorder) Name() string { return r.name }

func (r recorder) step(stage string) error {
	*r.calls = append(*r.calls, r.name+":"+stage)
	if r.failAt == stage {
		return errBroken
	}
	return nil
}

func (r recorder) Provision(context.Context) error { return r.step("provision") }
func (r recorder) Migrate(context.Context) error   { return r.step("migrate") }
func (r recorder) Reset(context.Context) error     { return r.step("reset") }

var errBroken = errors.New("broken")

func TestUpProvisionsThenMigratesInOrder(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls}, recorder{name: "es", calls: &calls})

	if err := set.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}

	want := "db:provision db:migrate es:provision es:migrate"
	if got := strings.Join(calls, " "); got != want {
		t.Fatalf("calls: %s, want %s", got, want)
	}
}

func TestUpRunsOncePerRun(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls})

	for range 3 {
		if err := set.Up(context.Background()); err != nil {
			t.Fatalf("up: %v", err)
		}
	}

	if len(calls) != 2 {
		t.Fatalf("calls: %v, want provision and migrate once", calls)
	}
}

func TestUpMemoizesTheFailure(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls, failAt: "provision"})

	first := set.Up(context.Background())
	second := set.Up(context.Background())

	if !errors.Is(first, errBroken) || !errors.Is(second, errBroken) {
		t.Fatalf("errors: %v, %v", first, second)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: %v, want one attempt", calls)
	}
}

func TestUpStopsAtTheFirstBrokenResource(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls, failAt: "provision"}, recorder{name: "es", calls: &calls})

	if err := set.Up(context.Background()); !errors.Is(err, errBroken) {
		t.Fatalf("up: %v", err)
	}
	if strings.Join(calls, " ") != "db:provision" {
		t.Fatalf("calls: %v, want nothing after the failure", calls)
	}
}

func TestResetProvisionsWhenUpWasNotCalled(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls})

	if err := set.Reset(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}

	want := "db:provision db:migrate db:reset"
	if got := strings.Join(calls, " "); got != want {
		t.Fatalf("calls: %s, want %s", got, want)
	}
}

func TestErrorNamesTheResourceAndTheStage(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls, failAt: "migrate"})

	err := set.Up(context.Background())
	if err == nil || err.Error() != "db: migrate: broken" {
		t.Fatalf("error: %v", err)
	}
}

type ephemeral struct {
	recorder
	failDown bool
}

func (e ephemeral) Down(context.Context) error {
	*e.calls = append(*e.calls, e.name+":down")
	if e.failDown {
		return errBroken
	}
	return nil
}

func TestDownRunsInReverseOrder(t *testing.T) {
	var calls []string
	set := New(
		ephemeral{recorder: recorder{name: "db", calls: &calls}},
		ephemeral{recorder: recorder{name: "es", calls: &calls}},
	)

	if err := set.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}

	if got := strings.Join(calls, " "); got != "es:down db:down" {
		t.Fatalf("calls: %s", got)
	}
}

func TestDownSkipsResourcesWithoutTeardown(t *testing.T) {
	var calls []string
	set := New(recorder{name: "db", calls: &calls})

	if err := set.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("calls: %v", calls)
	}
}

func TestDownKeepsGoingAfterAFailure(t *testing.T) {
	var calls []string
	set := New(
		ephemeral{recorder: recorder{name: "db", calls: &calls}},
		ephemeral{recorder: recorder{name: "es", calls: &calls}, failDown: true},
	)

	err := set.Down(context.Background())

	if !errors.Is(err, errBroken) {
		t.Fatalf("down: %v", err)
	}
	if got := strings.Join(calls, " "); got != "es:down db:down" {
		t.Fatalf("calls: %s, want the second one torn down anyway", got)
	}
}

type unnamed struct{}

func (unnamed) Provision(context.Context) error { return nil }
func (unnamed) Migrate(context.Context) error   { return nil }
func (unnamed) Reset(context.Context) error     { return errBroken }

func TestNameIsOptional(t *testing.T) {
	set := New(unnamed{})

	err := set.Reset(context.Background())
	if err == nil || err.Error() != "reset: broken" {
		t.Fatalf("error: %v", err)
	}
}
