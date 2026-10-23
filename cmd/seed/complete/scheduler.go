package complete

import (
	"container/heap"
	"time"
)

// Step is one business action executed at a simulated instant through a real service.
// A step may schedule follow-up steps (later instants), which is how multi-day workflows
// (stays, deferred payments, refunds...) are expressed. Steps whose instant is after the
// simulation horizon are never executed: their entity simply stays in its current state,
// which is exactly how "pending / in progress" objects exist in a real database.
type Step struct {
	At   time.Time
	Name string // domain label, used for progress/statistics
	Run  func(ctx *StepContext) error
	seq  uint64
}

type stepHeap []*Step

func (h stepHeap) Len() int { return len(h) }
func (h stepHeap) Less(i, j int) bool {
	if h[i].At.Equal(h[j].At) {
		return h[i].seq < h[j].seq
	}
	return h[i].At.Before(h[j].At)
}
func (h stepHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *stepHeap) Push(x any)   { *h = append(*h, x.(*Step)) }
func (h *stepHeap) Pop() any     { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }
func (h stepHeap) peek() (*Step, bool) {
	if len(h) == 0 {
		return nil, false
	}
	return h[0], true
}

// Scheduler orders steps deterministically by (instant, insertion order).
type Scheduler struct {
	h       stepHeap
	seq     uint64
	horizon time.Time
	dropped map[string]int
}

func NewScheduler(horizon time.Time) *Scheduler {
	return &Scheduler{horizon: horizon, dropped: map[string]int{}}
}

// At schedules fn at the given instant. Instants after the horizon are dropped (counted).
func (s *Scheduler) At(at time.Time, name string, fn func(ctx *StepContext) error) {
	if at.After(s.horizon) {
		s.dropped[name]++
		return
	}
	s.seq++
	heap.Push(&s.h, &Step{At: at, Name: name, Run: fn, seq: s.seq})
}

// popBefore returns the next step strictly before end, if any.
func (s *Scheduler) popBefore(end time.Time) (*Step, bool) {
	top, ok := s.h.peek()
	if !ok || !top.At.Before(end) {
		return nil, false
	}
	return heap.Pop(&s.h).(*Step), true
}

func (s *Scheduler) Pending() int { return len(s.h) }
