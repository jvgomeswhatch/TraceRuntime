package queue

import (
	"log/slog"
	"sync/atomic"
)

// Task is the unit of work passed from the HTTP handler to the worker.
// Traceparent carries the W3C traceparent so the worker creates a real child span.
type Task struct {
	ID          string
	TraceID     string
	Traceparent string
	Payload     string
}

// Queue is a bounded, non-blocking work queue.
// Enqueue returns false immediately if the queue is full.
type Queue struct {
	ch       chan Task
	capacity int
	rejected atomic.Int64
}

func NewQueue(capacity int) *Queue {
	return &Queue{
		ch:       make(chan Task, capacity),
		capacity: capacity,
	}
}

// Enqueue attempts to add a task. Returns false if the queue is full.
func (q *Queue) Enqueue(t Task) bool {
	select {
	case q.ch <- t:
		return true
	default:
		q.rejected.Add(1)
		slog.Warn("queue full — task rejected",
			"task_id", t.ID,
			"trace_id", t.TraceID,
			"depth", len(q.ch),
			"capacity", q.capacity,
		)
		return false
	}
}

// Receive returns the read channel for the worker. Not exported beyond this module.
func (q *Queue) Receive() <-chan Task {
	return q.ch
}

// Depth returns current number of tasks waiting.
func (q *Queue) Depth() int { return len(q.ch) }

// Capacity returns the maximum queue size.
func (q *Queue) Capacity() int { return q.capacity }

// Rejected returns total tasks rejected due to full queue.
func (q *Queue) Rejected() int64 { return q.rejected.Load() }
