package sim

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Scheduler runs state transitions at their due time on one goroutine. A
// transition only mutates in-memory state, so it is fast; slow work (webhook
// delivery, added in a later stage) goes to a bounded worker pool instead.
// The number of pending items is bounded by the number of live checkouts, and
// scheduler lag (how late a transition ran) is exported so the simulator can
// show whether it, rather than the application, became the bottleneck.
type Scheduler struct {
	mu    sync.Mutex
	items taskHeap
	seq   uint64
	wake  chan struct{}
	now   func() time.Time

	pending  atomic.Int64
	executed atomic.Int64
	lastLag  atomic.Int64 // nanoseconds
	maxLag   atomic.Int64 // nanoseconds
}

type task struct {
	at  time.Time
	seq uint64
	fn  func()
}

type taskHeap []task

func (h taskHeap) Len() int { return len(h) }
func (h taskHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}
func (h taskHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *taskHeap) Push(x any)   { *h = append(*h, x.(task)) }
func (h *taskHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func NewScheduler() *Scheduler {
	return &Scheduler{wake: make(chan struct{}, 1), now: time.Now}
}

// At schedules fn to run at the given time. Ties run in insertion order.
func (s *Scheduler) At(at time.Time, fn func()) {
	s.mu.Lock()
	s.seq++
	heap.Push(&s.items, task{at: at, seq: s.seq, fn: fn})
	s.pending.Add(1)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		s.mu.Lock()
		var next *task
		if len(s.items) > 0 {
			top := s.items[0]
			next = &top
		}
		s.mu.Unlock()

		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			}
			continue
		}

		wait := next.at.Sub(s.now())
		if wait > 0 {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return
			case <-s.wake: // an earlier task may have been added
			case <-timer.C:
			}
			continue
		}

		s.mu.Lock()
		if len(s.items) == 0 || s.items[0].seq != next.seq {
			s.mu.Unlock()
			continue
		}
		due := heap.Pop(&s.items).(task)
		s.mu.Unlock()

		lag := s.now().Sub(due.at)
		s.lastLag.Store(int64(lag))
		for {
			old := s.maxLag.Load()
			if int64(lag) <= old || s.maxLag.CompareAndSwap(old, int64(lag)) {
				break
			}
		}
		due.fn()
		s.pending.Add(-1)
		s.executed.Add(1)
	}
}

type SchedulerStats struct {
	Pending  int64   `json:"pending"`
	Executed int64   `json:"executed"`
	LagMS    float64 `json:"last_lag_ms"`
	MaxLagMS float64 `json:"max_lag_ms"`
}

func (s *Scheduler) Stats() SchedulerStats {
	return SchedulerStats{
		Pending:  s.pending.Load(),
		Executed: s.executed.Load(),
		LagMS:    float64(s.lastLag.Load()) / 1e6,
		MaxLagMS: float64(s.maxLag.Load()) / 1e6,
	}
}
