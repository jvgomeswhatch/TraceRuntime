package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type inferRequest struct {
	TaskID         string `json:"task_id"`
	Input          string `json:"input"`
	DeadlineUnixMs int64  `json:"deadline_unix_ms"`
}

type inferResponse struct {
	Output              string         `json:"output"`
	ExecutionStatus     string         `json:"execution_status"`
	InferenceDurationMs int            `json:"inference_duration_ms"`
	PromptTokens        int            `json:"prompt_tokens"`
	CompletionTokens    int            `json:"completion_tokens"`
	TokensPerSecond     float64        `json:"tokens_per_second"`
	ExecutionProfile    map[string]any `json:"execution_profile"`
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "ollama": "mocked"})
	})

	mux.HandleFunc("/infer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req inferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		// Trigger deterministic failure for chaos/error-path tests.
		if strings.Contains(req.Input, "trigger-failure") {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(inferResponse{
				Output:          "",
				ExecutionStatus: "failed",
			})
			return
		}

		// Simulate minimal inference latency.
		time.Sleep(50 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inferResponse{
			Output:              fmt.Sprintf("mock response for: %s", req.Input),
			ExecutionStatus:     "completed",
			InferenceDurationMs: 50,
			PromptTokens:        10,
			CompletionTokens:    20,
			TokensPerSecond:     400.0,
			ExecutionProfile:    map[string]any{"model": "mock"},
		})
	})

	log.Println("mock-ai-runtime listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", mux))
}
