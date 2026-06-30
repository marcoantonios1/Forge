package timeline

import (
	"sync"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
)

// Step is one buffered event, captured with its arrival time.
type Step struct {
	Timestamp time.Time
	Type      string
	Payload   map[string]any
}

// TimelineCollector implements events.Emitter by buffering every event it
// receives, in arrival order, with no side effects beyond storage. It never
// blocks, never errors, and is safe to wrap alongside the normal renderer
// via events.Multi — it does not replace live rendering, only adds a
// parallel buffer for post-task summarisation.
type TimelineCollector struct {
	mu    sync.Mutex
	steps []Step
}

// NewTimelineCollector returns an empty TimelineCollector.
func NewTimelineCollector() *TimelineCollector {
	return &TimelineCollector{}
}

// Emit implements events.Emitter.
func (c *TimelineCollector) Emit(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, Step{
		Timestamp: e.Timestamp,
		Type:      string(e.Type),
		Payload:   e.Payload,
	})
}

// Steps returns a copy of the buffered steps, in arrival order.
func (c *TimelineCollector) Steps() []Step {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Step, len(c.steps))
	copy(out, c.steps)
	return out
}
