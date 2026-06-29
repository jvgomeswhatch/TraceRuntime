package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestAlertCenter(t *testing.T) {
	cleanDB(t)

	token := envOr("INTERNAL_TOKEN", "")

	ctx := context.Background()
	for i := range 5 {
		status := "active"
		if i >= 3 {
			status = "resolved"
		}
		severity := "warning"
		if i == 0 {
			severity = "critical"
		}
		_, err := dbPool.Exec(ctx,
			`INSERT INTO healing_events (id, event_type, severity, source, status, details, created_at, resolved_at)
			 VALUES (gen_random_uuid(), $1, $2, 'test', $3, '{}', NOW() - ($4 || ' minutes')::interval,
			         CASE WHEN $3 = 'resolved' THEN NOW() ELSE NULL END)`,
			"worker.stale", severity, status, fmt.Sprintf("%d", (i+1)*5))
		if err != nil {
			t.Fatalf("insert healing event: %v", err)
		}
	}

	t.Run("ListAll", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/alerts?status=all&limit=10", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		alerts := result["alerts"].([]any)
		if len(alerts) < 5 {
			t.Fatalf("expected at least 5 alerts, got %d", len(alerts))
		}
	})

	t.Run("FilterByStatus", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/alerts?status=active", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		alerts := result["alerts"].([]any)
		for _, a := range alerts {
			alert := a.(map[string]any)
			if alert["status"] != "active" {
				t.Fatalf("expected status active, got %v", alert["status"])
			}
		}
	})

	t.Run("Acknowledge", func(t *testing.T) {
		var alertID string
		err := dbPool.QueryRow(ctx, "SELECT id FROM healing_events WHERE status = 'active' LIMIT 1").Scan(&alertID)
		if err != nil {
			t.Fatalf("get active alert: %v", err)
		}

		req, _ := http.NewRequest("POST", apiURL+"/api/alerts/"+alertID+"/acknowledge", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}

		var status string
		dbPool.QueryRow(ctx, "SELECT status FROM healing_events WHERE id = $1", alertID).Scan(&status)
		if status != "acknowledged" {
			t.Fatalf("expected acknowledged, got %s", status)
		}
	})

	t.Run("AcknowledgeResolved", func(t *testing.T) {
		var alertID string
		err := dbPool.QueryRow(ctx, "SELECT id FROM healing_events WHERE status = 'resolved' LIMIT 1").Scan(&alertID)
		if err != nil {
			t.Fatalf("get resolved alert: %v", err)
		}
		req, _ := http.NewRequest("POST", apiURL+"/api/alerts/"+alertID+"/acknowledge", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("expected 409 for resolved alert, got %d", resp.StatusCode)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/alerts/stats", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["by_type"] == nil {
			t.Fatal("expected by_type in stats")
		}
	})

	_ = time.Now()
}
