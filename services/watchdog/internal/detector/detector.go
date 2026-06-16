package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/runtime-platform/services/watchdog/internal/config"
	"github.com/runtime-platform/services/watchdog/internal/db"
	"github.com/runtime-platform/services/watchdog/internal/metrics"
	"github.com/runtime-platform/services/watchdog/internal/publisher"
)

// Detector is the core detection engine. It polls for anomalies and emits
// healing events. It is an observer only -- it does NOT restart workers,
// requeue tasks, or throttle anything.
type Detector struct {
	cfg       *config.Config
	db        *db.DB
	publisher *publisher.SSEPublisher
	sqsClient *sqs.Client
	httpClient *http.Client

	// active tracks dedup keys -> healing_event IDs so the same condition
	// never produces duplicate events. Survives watchdog restarts via
	// reconstructActiveState which re-loads from PostgreSQL.
	active map[string]activeEntry
	mu     sync.Mutex

	healthFailures int // consecutive worker health-check failures
}

type activeEntry struct {
	eventID   string
	eventType string
}

// New creates a Detector and reconstructs its active-event map from
// PostgreSQL so restarts are seamless.
func New(cfg *config.Config, database *db.DB, pub *publisher.SSEPublisher, sqsClient *sqs.Client) *Detector {
	d := &Detector{
		cfg:       cfg,
		db:        database,
		publisher: pub,
		sqsClient: sqsClient,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		active:    make(map[string]activeEntry),
	}

	if err := d.reconstructActiveState(); err != nil {
		slog.Error("detector.reconstruct_active_state failed, starting with empty map",
			"error", err)
	}

	return d
}

// reconstructActiveState loads all active healing events from PostgreSQL and
// populates the in-memory dedup map. This ensures the watchdog picks up where
// it left off after a restart.
func (d *Detector) reconstructActiveState() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := d.db.ListActiveHealingEvents(ctx)
	if err != nil {
		return fmt.Errorf("list active healing events: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, ev := range events {
		key := d.keyForEvent(ev)
		if key != "" {
			d.active[key] = activeEntry{eventID: ev.ID, eventType: ev.EventType}
		}
	}

	slog.Info("detector.reconstructed_active_state",
		"active_count", len(d.active))
	return nil
}

// keyForEvent derives the dedup key from a persisted healing event.
func (d *Detector) keyForEvent(ev db.HealingEvent) string {
	switch ev.EventType {
	case "worker.stale":
		if ev.WorkerID != "" {
			return "worker.stale:" + ev.WorkerID
		}
		// Fallback: try to extract from details
		var det map[string]interface{}
		if json.Unmarshal(ev.Details, &det) == nil {
			if wid, ok := det["worker_id"].(string); ok {
				return "worker.stale:" + wid
			}
		}
		return ""
	case "worker.down":
		return "worker.down"
	case "queue.lag":
		return "queue.lag"
	case "task.stuck":
		var det map[string]interface{}
		if json.Unmarshal(ev.Details, &det) == nil {
			if tid, ok := det["task_id"].(string); ok {
				return "task.stuck:" + tid
			}
		}
		return ""
	case "dlq.nonempty":
		return "dlq.nonempty"
	default:
		return ""
	}
}

// Run starts the main polling loop. It blocks until ctx is cancelled.
func (d *Detector) Run(ctx context.Context) {
	interval := time.Duration(d.cfg.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("detector.run started",
		"poll_interval_seconds", d.cfg.PollIntervalSeconds)

	// Run one poll immediately at startup so we don't wait an entire
	// interval before first detection.
	d.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("detector.run stopping")
			return
		case <-ticker.C:
			d.poll(ctx)
		}
	}
}

func (d *Detector) poll(ctx context.Context) {
	start := time.Now()
	d.detectStaleWorkers(ctx)
	d.detectWorkerDown(ctx)
	d.detectQueueLag(ctx)
	d.detectStuckTasks(ctx)
	d.detectDLQ(ctx)

	elapsed := time.Since(start).Seconds()
	metrics.PollDuration.Observe(elapsed)

	d.mu.Lock()
	active := len(d.active)
	d.mu.Unlock()
	metrics.ActiveIncidents.Set(float64(active))

	slog.Info("detector.poll_completed", "duration_seconds", elapsed, "active_incidents", active)
}

// ---------------------------------------------------------------------------
// Detection functions
// ---------------------------------------------------------------------------

func (d *Detector) detectStaleWorkers(ctx context.Context) {
	heartbeats, err := d.db.ListHeartbeats(ctx)
	if err != nil {
		slog.Error("detector.stale_workers query failed", "error", err)
		return
	}

	threshold := time.Duration(d.cfg.HeartbeatStaleSeconds) * time.Second
	staleCount := 0

	// Track which workers are currently stale so we can resolve ones that recovered.
	currentlyStale := make(map[string]bool)

	for _, hb := range heartbeats {
		gap := time.Since(hb.LastSeenAt)
		key := "worker.stale:" + hb.WorkerID

		if gap > threshold {
			staleCount++
			currentlyStale[hb.WorkerID] = true

			d.mu.Lock()
			_, exists := d.active[key]
			d.mu.Unlock()

			if !exists {
				details, _ := json.Marshal(map[string]interface{}{
					"worker_id":   hb.WorkerID,
					"last_seen":   hb.LastSeenAt.Format(time.RFC3339),
					"gap_seconds": int(gap.Seconds()),
				})
				d.emit(ctx, key, "worker.stale", "warning", "watchdog", hb.WorkerID, details)
			}
		}
	}

	// Resolve stale events for workers that have recovered.
	d.mu.Lock()
	var toResolve []string
	for key := range d.active {
		if !strings.HasPrefix(key, "worker.stale:") {
			continue
		}
		workerID := strings.TrimPrefix(key, "worker.stale:")
		if !currentlyStale[workerID] {
			toResolve = append(toResolve, key)
		}
	}
	d.mu.Unlock()

	for _, key := range toResolve {
		d.resolve(ctx, key)
	}

	metrics.WorkersStale.Set(float64(staleCount))
	metrics.WorkersTotal.Set(float64(len(heartbeats)))
	metrics.WorkersHealthy.Set(float64(len(heartbeats) - staleCount))
}

func (d *Detector) detectWorkerDown(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.cfg.WorkerHealthURL, nil)
	if err != nil {
		slog.Error("detector.worker_down request build failed", "error", err)
		d.healthFailures++
		d.evaluateWorkerDown(ctx)
		return
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.healthFailures++
		slog.Warn("detector.worker_down health check failed",
			"error", err,
			"consecutive_failures", d.healthFailures)
		d.evaluateWorkerDown(ctx)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		d.healthFailures++
		slog.Warn("detector.worker_down health check returned error",
			"status", resp.StatusCode,
			"consecutive_failures", d.healthFailures)
		d.evaluateWorkerDown(ctx)
		return
	}

	// Health check succeeded -- reset counter and resolve active event.
	if d.healthFailures > 0 {
		slog.Info("detector.worker_down health check recovered",
			"previous_failures", d.healthFailures)
	}
	d.healthFailures = 0

	d.mu.Lock()
	_, exists := d.active["worker.down"]
	d.mu.Unlock()

	if exists {
		d.resolve(ctx, "worker.down")
	}

	metrics.WorkersDown.Set(0)
}

func (d *Detector) evaluateWorkerDown(ctx context.Context) {
	if d.healthFailures < d.cfg.HealthcheckFailures {
		return
	}

	key := "worker.down"
	d.mu.Lock()
	_, exists := d.active[key]
	d.mu.Unlock()

	if exists {
		return // already reported
	}

	details, _ := json.Marshal(map[string]interface{}{
		"consecutive_failures": d.healthFailures,
		"threshold":            d.cfg.HealthcheckFailures,
		"health_url":           d.cfg.WorkerHealthURL,
	})
	d.emit(ctx, key, "worker.down", "critical", "watchdog", "", details)
	metrics.WorkersDown.Set(1)
}

func (d *Detector) detectQueueLag(ctx context.Context) {
	depth, err := d.getQueueDepth(ctx, d.cfg.SQSQueueURL)
	if err != nil {
		slog.Error("detector.queue_lag sqs query failed", "error", err)
		return
	}

	metrics.QueueLag.Set(float64(depth))
	key := "queue.lag"

	if depth > d.cfg.QueueLagThreshold {
		d.mu.Lock()
		_, exists := d.active[key]
		d.mu.Unlock()

		if !exists {
			details, _ := json.Marshal(map[string]interface{}{
				"depth":     depth,
				"threshold": d.cfg.QueueLagThreshold,
			})
			d.emit(ctx, key, "queue.lag", "warning", "watchdog", "", details)
		}
	} else {
		d.mu.Lock()
		_, exists := d.active[key]
		d.mu.Unlock()

		if exists {
			d.resolve(ctx, key)
		}
	}
}

func (d *Detector) detectStuckTasks(ctx context.Context) {
	stuck, err := d.db.StuckTasks(ctx, d.cfg.TaskStuckSeconds)
	if err != nil {
		slog.Error("detector.stuck_tasks query failed", "error", err)
		return
	}

	metrics.StuckTasks.Set(float64(len(stuck)))

	// Track which tasks are currently stuck for resolution detection.
	currentlyStuck := make(map[string]bool, len(stuck))

	for _, st := range stuck {
		key := "task.stuck:" + st.TaskID
		currentlyStuck[st.TaskID] = true

		d.mu.Lock()
		_, exists := d.active[key]
		d.mu.Unlock()

		if !exists {
			stuckDuration := time.Since(st.ProcessingStartedAt)
			details, _ := json.Marshal(map[string]interface{}{
				"task_id":        st.TaskID,
				"trace_id":       st.TraceID,
				"stuck_seconds":  int(stuckDuration.Seconds()),
				"started_at":     st.ProcessingStartedAt.Format(time.RFC3339),
			})
			d.emit(ctx, key, "task.stuck", "warning", "watchdog", "", details)
		}
	}

	// Resolve task.stuck events for tasks that are no longer stuck.
	d.mu.Lock()
	var toResolve []string
	for key := range d.active {
		if !strings.HasPrefix(key, "task.stuck:") {
			continue
		}
		taskID := strings.TrimPrefix(key, "task.stuck:")
		if !currentlyStuck[taskID] {
			toResolve = append(toResolve, key)
		}
	}
	d.mu.Unlock()

	for _, key := range toResolve {
		d.resolve(ctx, key)
	}
}

func (d *Detector) detectDLQ(ctx context.Context) {
	depth, err := d.getQueueDepth(ctx, d.cfg.SQSDlqURL)
	if err != nil {
		slog.Error("detector.dlq sqs query failed", "error", err)
		return
	}

	metrics.DLQDepth.Set(float64(depth))
	key := "dlq.nonempty"

	if depth > 0 {
		d.mu.Lock()
		_, exists := d.active[key]
		d.mu.Unlock()

		if !exists {
			details, _ := json.Marshal(map[string]interface{}{
				"depth": depth,
			})
			d.emit(ctx, key, "dlq.nonempty", "critical", "watchdog", "", details)
		}
	} else {
		d.mu.Lock()
		_, exists := d.active[key]
		d.mu.Unlock()

		if exists {
			d.resolve(ctx, key)
		}
	}
}

// ---------------------------------------------------------------------------
// SQS helper
// ---------------------------------------------------------------------------

func (d *Detector) getQueueDepth(ctx context.Context, queueURL string) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := d.sqsClient.GetQueueAttributes(reqCtx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			sqstypes.QueueAttributeNameApproximateNumberOfMessages,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("sqs.GetQueueAttributes(%s): %w", queueURL, err)
	}

	valStr, ok := out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]
	if !ok {
		return 0, nil
	}

	n, err := strconv.Atoi(valStr)
	if err != nil {
		return 0, fmt.Errorf("parse queue depth %q: %w", valStr, err)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Emit / Resolve helpers
// ---------------------------------------------------------------------------

// emit creates a new healing event in DB, publishes it to SSE, and registers
// it in the active dedup map.
func (d *Detector) emit(ctx context.Context, key, eventType, severity, source, workerID string, details json.RawMessage) {
	id, err := d.db.InsertHealingEvent(ctx, eventType, severity, source, workerID, details)
	if err != nil {
		slog.Error("detector.emit db insert failed",
			"event_type", eventType,
			"key", key,
			"error", err)
		return
	}

	sseEvent := publisher.SSEEvent{
		EventType:      "healing." + eventType,
		Severity:       severity,
		Status:         "active",
		HealingEventID: id,
		Details:        details,
	}
	if err := d.publisher.Publish(ctx, sseEvent); err != nil {
		slog.Warn("detector.emit sse publish failed",
			"event_type", eventType,
			"error", err)
		// Don't return -- the DB event was created. SSE is best-effort.
	}

	d.mu.Lock()
	d.active[key] = activeEntry{eventID: id, eventType: eventType}
	d.mu.Unlock()

	metrics.HealingEventsTotal.WithLabelValues(eventType, severity).Inc()

	slog.Info("detector.healing_event_created",
		"key", key,
		"event_type", eventType,
		"severity", severity,
		"healing_event_id", id)
}

// resolve marks a healing event as resolved in DB, publishes resolution to
// SSE, and removes it from the active dedup map.
func (d *Detector) resolve(ctx context.Context, key string) {
	d.mu.Lock()
	entry, exists := d.active[key]
	if !exists {
		d.mu.Unlock()
		return
	}
	delete(d.active, key)
	d.mu.Unlock()

	if err := d.db.ResolveHealingEvent(ctx, entry.eventID); err != nil {
		slog.Error("detector.resolve db update failed",
			"key", key,
			"healing_event_id", entry.eventID,
			"error", err)
		return
	}

	resolvedDetails, _ := json.Marshal(map[string]interface{}{
		"resolved_key": key,
	})

	sseEvent := publisher.SSEEvent{
		EventType:      "healing." + entry.eventType + ".resolved",
		Status:         "resolved",
		HealingEventID: entry.eventID,
		Details:        resolvedDetails,
	}
	if err := d.publisher.Publish(ctx, sseEvent); err != nil {
		slog.Warn("detector.resolve sse publish failed",
			"key", key,
			"error", err)
	}

	metrics.HealingEventsResolved.WithLabelValues(entry.eventType).Inc()

	slog.Info("detector.healing_event_resolved",
		"key", key,
		"event_type", entry.eventType,
		"healing_event_id", entry.eventID)
}
