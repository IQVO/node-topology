// Package usecases holds the application's use cases, one per operation of
// apis/openapi.yaml. Every command follows one shape inside ONE
// ports.UnitOfWork: load the aggregates, call the domain, save them and append
// the raised events to the outbox, then return a view. Domain, shared and port
// errors pass through unchanged (errors.Is keeps working): the HTTP adapter
// maps them to problem types later.
package usecases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// Deps bundles the ports the command use cases need.
type Deps struct {
	Nodes        ports.NodeRepository
	Lanes        ports.LaneRepository
	ServiceAreas ports.ServiceAreaRepository
	Regions      ports.RegionRepository
	Outbox       ports.EventOutbox
	UoW          ports.UnitOfWork
	Clock        ports.Clock
	IDs          ports.IDGenerator
	// Metrics records the business outcome of each node registration; nil
	// records nothing, so unit tests need no wiring.
	Metrics ports.NodeMetrics
}

// metrics returns the configured NodeMetrics or a no-op.
func (d Deps) metrics() ports.NodeMetrics {
	if d.Metrics != nil {
		return d.Metrics
	}
	return noMetrics{}
}

// noMetrics is the NodeMetrics of a Deps without one.
type noMetrics struct{}

func (noMetrics) NodeRegistered(context.Context)       {}
func (noMetrics) NodeRejected(context.Context, string) {}

// writer is what every command embeds.
type writer struct{ Deps }

// now returns the clock's current time in UTC.
func (w writer) now() time.Time { return w.Clock.Now().UTC() }

// publish appends the raised events to the outbox, on the ctx of the unit of
// work.
func (w writer) publish(ctx context.Context, events []shared.Event) error {
	if err := w.Outbox.Append(ctx, events...); err != nil {
		return fmt.Errorf("append events to outbox: %w", err)
	}
	return nil
}

// transact runs attempt in a unit of work. A ports.ErrConcurrentUpdate means
// another writer got in between the load and the save, so the whole attempt
// (load, domain call, save, outbox) runs once more on fresh state before the
// conflict is reported (ADR 0003, decision 4).
func transact[T any](ctx context.Context, uow ports.UnitOfWork, attempt func(ctx context.Context) (T, error)) (T, error) {
	var out T
	run := func(ctx context.Context) error {
		v, err := attempt(ctx)
		if err != nil {
			return err
		}
		out = v
		return nil
	}
	err := uow.Do(ctx, run)
	if errors.Is(err, ports.ErrConcurrentUpdate) {
		err = uow.Do(ctx, run)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// mutateNode loads a node, applies one domain command to it and persists the
// result with its events.
func (w writer) mutateNode(
	ctx context.Context, id node.ID,
	apply func(n *node.Node, now time.Time) ([]shared.Event, error),
) (views.Node, error) {
	return transact(ctx, w.UoW, func(ctx context.Context) (views.Node, error) {
		n, err := w.Nodes.Get(ctx, id)
		if err != nil {
			return views.Node{}, err
		}
		events, err := apply(n, w.now())
		if err != nil {
			return views.Node{}, err
		}
		if err := w.Nodes.Save(ctx, n); err != nil {
			return views.Node{}, err
		}
		if err := w.publish(ctx, events); err != nil {
			return views.Node{}, err
		}
		return views.NodeFrom(n), nil
	})
}

// found reports whether a lookup hit: a nil error is a hit, ports.ErrNotFound
// a miss, anything else a failure.
func found(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ports.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// optionalRegion loads a region; a miss is a nil region and no error, because
// the domain turns it into region.ErrUnknownRegion (ADR 0002, A16).
func optionalRegion(ctx context.Context, regions ports.RegionRepository, id region.ID) (*region.Region, error) {
	r, err := regions.Get(ctx, id)
	ok, err := found(err)
	if err != nil || !ok {
		return nil, err
	}
	return r, nil
}
