package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

type HealingEvent struct {
	ID         string
	EventType  string
	Severity   string
	Source     string
	Status     string
	WorkerID   string
	Details    json.RawMessage
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

type Heartbeat struct {
	WorkerID   string
	LastSeenAt time.Time
}

type Checker struct {
	db           *pgxpool.Pool
	docker       *docker.Controller
	sqs          *sqs.Client
	httpClient   *http.Client
	apiURL       string
	workerURL    string
	aiRuntimeURL string
	queueURL     string
	dlqURL       string
}

func New(db *pgxpool.Pool, dc *docker.Controller, sqsClient *sqs.Client, apiURL, workerURL, aiRuntimeURL, queueURL, dlqURL string) *Checker {
	return &Checker{
		db:           db,
		docker:       dc,
		sqs:          sqsClient,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		apiURL:       apiURL,
		workerURL:    workerURL,
		aiRuntimeURL: aiRuntimeURL,
		queueURL:     queueURL,
		dlqURL:       dlqURL,
	}
}

// --- PostgreSQL ---

func (c *Checker) ActiveHealingEvents(ctx context.Context) ([]HealingEvent, error) {
	rows, err := c.db.Query(ctx,
		`SELECT id::text, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events WHERE status = 'active' ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query active healing events: %w", err)
	}
	defer rows.Close()

	var events []HealingEvent
	for rows.Next() {
		var e HealingEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.Source, &e.Status, &e.WorkerID, &e.Details, &e.CreatedAt, &e.ResolvedAt); err != nil {
			return nil, fmt.Errorf("scan healing event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (c *Checker) HealingEventsSince(ctx context.Context, since time.Time) ([]HealingEvent, error) {
	rows, err := c.db.Query(ctx,
		`SELECT id::text, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events WHERE created_at >= $1 ORDER BY created_at ASC`, since)
	if err != nil {
		return nil, fmt.Errorf("query healing events since: %w", err)
	}
	defer rows.Close()

	var events []HealingEvent
	for rows.Next() {
		var e HealingEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.Source, &e.Status, &e.WorkerID, &e.Details, &e.CreatedAt, &e.ResolvedAt); err != nil {
			return nil, fmt.Errorf("scan healing event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (c *Checker) TaskStatus(ctx context.Context, taskID string) (string, error) {
	var status string
	err := c.db.QueryRow(ctx, `SELECT status FROM tasks WHERE id = $1::uuid`, taskID).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("query task status: %w", err)
	}
	return status, nil
}

func (c *Checker) TasksInStatus(ctx context.Context, status string) (int, error) {
	var count int
	err := c.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE status = $1`, status).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query tasks in status: %w", err)
	}
	return count, nil
}

func (c *Checker) TasksInStatusSince(ctx context.Context, status string, since time.Time) (int, error) {
	var count int
	err := c.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE status = $1 AND created_at >= $2`, status, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query tasks in status since: %w", err)
	}
	return count, nil
}

func (c *Checker) LatestHeartbeat(ctx context.Context) (*Heartbeat, error) {
	var hb Heartbeat
	err := c.db.QueryRow(ctx,
		`SELECT worker_id, last_seen_at FROM worker_heartbeats ORDER BY last_seen_at DESC LIMIT 1`,
	).Scan(&hb.WorkerID, &hb.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("query latest heartbeat: %w", err)
	}
	return &hb, nil
}

// HeartbeatAgeSec returns the age of the most recent heartbeat in seconds,
// computed by PostgreSQL (NOW() - last_seen_at) to avoid host/container clock drift.
func (c *Checker) HeartbeatAgeSec(ctx context.Context) (float64, error) {
	var ageSec float64
	err := c.db.QueryRow(ctx,
		`SELECT EXTRACT(EPOCH FROM (NOW() - last_seen_at))
		 FROM worker_heartbeats ORDER BY last_seen_at DESC LIMIT 1`,
	).Scan(&ageSec)
	if err != nil {
		return 0, fmt.Errorf("query heartbeat age: %w", err)
	}
	return ageSec, nil
}

func (c *Checker) ResolveAllActiveEvents(ctx context.Context) error {
	_, err := c.db.Exec(ctx, `UPDATE healing_events SET status = 'resolved', resolved_at = NOW() WHERE status = 'active'`)
	return err
}

func (c *Checker) DeleteStaleHeartbeats(ctx context.Context, maxAge time.Duration) (int64, error) {
	tag, err := c.db.Exec(ctx,
		`DELETE FROM worker_heartbeats WHERE last_seen_at < NOW() - make_interval(secs => $1)`,
		int(maxAge.Seconds()),
	)
	if err != nil {
		return 0, fmt.Errorf("delete stale heartbeats: %w", err)
	}
	return tag.RowsAffected(), nil
}

// --- Docker ---

func (c *Checker) ContainerHealth(ctx context.Context, service string) (docker.HealthStatus, error) {
	return c.docker.Health(ctx, service)
}

func (c *Checker) ContainerRestartCount(ctx context.Context, service string) (int, error) {
	return c.docker.RestartCount(ctx, service)
}

func (c *Checker) AllContainersHealthy(ctx context.Context, names []string) (bool, error) {
	for _, name := range names {
		status, err := c.docker.Health(ctx, name)
		if err != nil {
			return false, err
		}
		if status != docker.HealthHealthy {
			return false, nil
		}
	}
	return true, nil
}

// --- SQS ---

func (c *Checker) QueueDepth(ctx context.Context) (int, error) {
	return c.queueAttribute(ctx, c.queueURL, sqstypes.QueueAttributeNameApproximateNumberOfMessages)
}

func (c *Checker) DLQDepth(ctx context.Context) (int, error) {
	return c.queueAttribute(ctx, c.dlqURL, sqstypes.QueueAttributeNameApproximateNumberOfMessages)
}

func (c *Checker) queueAttribute(ctx context.Context, queueURL string, attr sqstypes.QueueAttributeName) (int, error) {
	out, err := c.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{attr},
	})
	if err != nil {
		return 0, fmt.Errorf("sqs get attributes: %w", err)
	}
	val, ok := out.Attributes[string(attr)]
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("parse queue attribute: %w", err)
	}
	return n, nil
}

func (c *Checker) PurgeQueue(ctx context.Context) error {
	_, err := c.sqs.PurgeQueue(ctx, &sqs.PurgeQueueInput{
		QueueUrl: aws.String(c.queueURL),
	})
	return err
}

// --- HTTP ---

func (c *Checker) httpHealth(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/health", nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func (c *Checker) APIHealth(ctx context.Context) (int, error) {
	return c.httpHealth(ctx, c.apiURL)
}

func (c *Checker) WorkerHealth(ctx context.Context) (int, error) {
	return c.httpHealth(ctx, c.workerURL)
}

func (c *Checker) AIRuntimeHealth(ctx context.Context) (int, error) {
	return c.httpHealth(ctx, c.aiRuntimeURL)
}

// --- Polling ---

type WaitCondition struct {
	Name     string
	Check    func(ctx context.Context) (bool, error)
	Timeout  time.Duration
	Interval time.Duration
}

type WaitResult struct {
	Elapsed  time.Duration
	Attempts int
	Met      bool
}

func (c *Checker) WaitFor(ctx context.Context, cond WaitCondition) (*WaitResult, error) {
	start := time.Now()
	deadline := time.After(cond.Timeout)
	interval := cond.Interval
	if interval == 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	attempts := 0
	for {
		select {
		case <-ctx.Done():
			return &WaitResult{Elapsed: time.Since(start), Attempts: attempts, Met: false}, ctx.Err()
		case <-deadline:
			return &WaitResult{Elapsed: time.Since(start), Attempts: attempts, Met: false}, nil
		case <-ticker.C:
			attempts++
			met, err := cond.Check(ctx)
			if err != nil {
				slog.Warn("wait condition check error", "name", cond.Name, "attempt", attempts, "error", err)
				continue
			}
			if met {
				return &WaitResult{Elapsed: time.Since(start), Attempts: attempts, Met: true}, nil
			}
		}
	}
}
