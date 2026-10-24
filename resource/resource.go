// Package resource is the seam between a component test and the external
// systems it runs against for real.
package resource

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Resource interface {
	Provision(ctx context.Context) error
	Migrate(ctx context.Context) error
	Reset(ctx context.Context) error
}

type Named interface {
	Name() string
}

type Ephemeral interface {
	Down(ctx context.Context) error
}

// All a step gets of a resource: Provision and Reset belong to the suite.
type Fault interface {
	FailWith(err error)
}

func New(resources ...Resource) *Set {
	return &Set{resources: resources}
}

// Down ends the run: the once is not reset, so Up does not provision again.
type Set struct {
	resources []Resource
	once      sync.Once
	err       error
}

func (s *Set) Up(ctx context.Context) error {
	s.once.Do(func() {
		for _, r := range s.resources {
			s.err = wrap(r, "provision", r.Provision(ctx))
			if s.err != nil {
				return
			}
			s.err = wrap(r, "migrate", r.Migrate(ctx))
			if s.err != nil {
				return
			}
		}
	})
	// Memoized, so a hundred scenarios report the first cause instead of
	// re-diagnosing an absent server each time.
	return s.err
}

// Reset calls Up first, so a suite that only ever calls Reset is still correct.
func (s *Set) Reset(ctx context.Context) error {
	if err := s.Up(ctx); err != nil {
		return err
	}
	for _, r := range s.resources {
		if err := wrap(r, "reset", r.Reset(ctx)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Set) Down(ctx context.Context) error {
	var errs []error
	// Reverse, and without stopping: the run is over, so clean up as much as
	// possible rather than as little.
	for i := len(s.resources) - 1; i >= 0; i-- {
		ephemeral, ok := s.resources[i].(Ephemeral)
		if !ok {
			continue
		}
		if err := wrap(s.resources[i], "down", ephemeral.Down(ctx)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func wrap(r Resource, stage string, err error) error {
	if err == nil {
		return nil
	}
	if named, ok := r.(Named); ok {
		return fmt.Errorf("%s: %s: %w", named.Name(), stage, err)
	}
	return fmt.Errorf("%s: %w", stage, err)
}
