package memory

import (
	"fmt"
	"sync"
	"time"

	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// Clock is a settable ports.Clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock reading t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now returns the current reading.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// SequentialIDs is a ports.IDGenerator minting node-1, node-2, lane-1,
// region-1, ... one counter per kind.
type SequentialIDs struct {
	mu                    sync.Mutex
	nodes, lanes, regions int
}

// NewNodeID returns the next node id.
func (g *SequentialIDs) NewNodeID() node.ID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes++
	return node.ID(fmt.Sprintf("node-%d", g.nodes))
}

// NewLaneID returns the next lane id.
func (g *SequentialIDs) NewLaneID() lane.ID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lanes++
	return lane.ID(fmt.Sprintf("lane-%d", g.lanes))
}

// NewRegionID returns the next region id.
func (g *SequentialIDs) NewRegionID() region.ID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.regions++
	return region.ID(fmt.Sprintf("region-%d", g.regions))
}
