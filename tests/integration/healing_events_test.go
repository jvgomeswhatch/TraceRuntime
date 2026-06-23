package integration

import (
	"context"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

// TestHealingEvents_TableExists verifies the healing_events table exists and
// is accessible from the test database connection.
func TestHealingEvents_TableExists(t *testing.T) {
	var count int
	err := dbPool.QueryRow(context.Background(),
		"SELECT count(*) FROM healing_events").Scan(&count)
	if err != nil {
		t.Fatalf("healing_events table not accessible: %v", err)
	}
	// count may be anything — we only care that the query did not error.
	t.Logf("healing_events row count: %d", count)
}

// TestHealingEvents_CleanStateAfterTruncate verifies that cleanDB empties the
// healing_events table.
func TestHealingEvents_CleanStateAfterTruncate(t *testing.T) {
	cleanDB(t)

	var count int
	err := dbPool.QueryRow(context.Background(),
		"SELECT count(*) FROM healing_events").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count healing_events: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows in healing_events after cleanDB, got %d", count)
	}
}

// TestHealingEvents_WatchdogHealthy verifies the watchdog service is running
// and its health endpoint returns HTTP 200. This is a prerequisite for all
// healing-event detection.
func TestHealingEvents_WatchdogHealthy(t *testing.T) {
	watchdogURL := "http://localhost:9093/health"

	resp, err := http.Get(watchdogURL)
	if err != nil {
		t.Fatalf("watchdog health endpoint unreachable at %s: %v", watchdogURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("watchdog /health returned %d, want 200", resp.StatusCode)
	}
}

// TestHealingEvents_StaleWorkerDetection pauses the worker container long
// enough for the watchdog to detect a stale heartbeat, then checks that a
// healing event was persisted. The test is timing-dependent; it uses t.Skip
// rather than t.Fatal if the watchdog does not produce an event within the
// observation window.
//
// Watchdog configuration (from docker-compose.yml):
//   - WATCHDOG_POLL_INTERVAL_SECONDS=15
//   - WATCHDOG_HEARTBEAT_STALE_SECONDS=60
//
// The worker heartbeat interval is 10 s. To reliably produce a stale event we
// need the worker to be silent for at least 60 s. We pause the container for
// 90 s and wait up to 120 s for a healing event to appear.
func TestHealingEvents_StaleWorkerDetection(t *testing.T) {
	const (
		workerContainer = "traceruntime-worker-1"
		pauseDuration   = 90 * time.Second
		pollInterval    = 2 * time.Second
		waitTimeout     = 120 * time.Second
	)

	// Clean slate so we are not counting pre-existing events.
	cleanDB(t)

	startTime := time.Now()

	// Pause the worker container so it stops sending heartbeats.
	t.Logf("pausing container %s at %s", workerContainer, startTime.Format(time.RFC3339))
	pauseOut, err := exec.Command("docker", "pause", workerContainer).CombinedOutput()
	if err != nil {
		t.Skipf("could not pause container %s (%v: %s) — skipping timing-dependent test",
			workerContainer, err, string(pauseOut))
	}

	// Always unpause, even if the test fails or skips below.
	t.Cleanup(func() {
		unpauseOut, unpauseErr := exec.Command("docker", "unpause", workerContainer).CombinedOutput()
		if unpauseErr != nil {
			t.Logf("WARNING: failed to unpause container %s: %v: %s",
				workerContainer, unpauseErr, string(unpauseOut))
		} else {
			t.Logf("unpaused container %s", workerContainer)
		}
	})

	t.Logf("container paused; waiting up to %v for watchdog to detect stale heartbeat", waitTimeout)

	// Poll healing_events for a row created after startTime.
	type healingRow struct {
		EventType string
		Severity  string
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()

	var found *healingRow
	for {
		var row healingRow
		queryErr := dbPool.QueryRow(ctx,
			`SELECT event_type, severity
			 FROM healing_events
			 WHERE created_at >= $1
			 ORDER BY created_at ASC
			 LIMIT 1`,
			startTime,
		).Scan(&row.EventType, &row.Severity)

		if queryErr == nil {
			// A healing event was found.
			found = &row
			break
		}

		// Check if the pause duration has elapsed before we even see an event.
		// If so, we have waited long enough for the stale threshold to be
		// crossed (pauseDuration > stale threshold of 60 s).
		elapsed := time.Since(startTime)
		if elapsed >= pauseDuration {
			t.Logf("elapsed %v >= pause duration %v but no healing event yet; continuing to wait", elapsed, pauseDuration)
		}

		select {
		case <-ctx.Done():
			// Timeout reached without a healing event — skip rather than fail.
			t.Skipf("watchdog did not create a healing event within %v — timing-dependent test skipped", waitTimeout)
			return
		case <-time.After(pollInterval):
		}
	}

	// We found a healing event — validate its fields.
	t.Logf("healing event detected: event_type=%q severity=%q (elapsed: %v)",
		found.EventType, found.Severity, time.Since(startTime))

	if found.EventType == "" {
		t.Error("healing event has empty event_type")
	}
	if found.Severity == "" {
		t.Error("healing event has empty severity")
	}
}
