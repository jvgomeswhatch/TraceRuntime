package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runtime-platform/services/worker/internal/db"
	"github.com/runtime-platform/services/worker/internal/metrics"
	"github.com/runtime-platform/services/worker/internal/processor"
	"github.com/runtime-platform/services/worker/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-worker")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			slog.Error("tracer shutdown error", "error", err)
		}
	}()

	database, err := db.Open(context.Background(), mustEnv("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	queueURL := mustEnv("SQS_QUEUE_URL")
	dlqURL := mustEnv("SQS_DLQ_URL")

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	sqsClient := sqs.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	proc := processor.New(processor.Config{
		SQSURL:         queueURL,
		S3Bucket:       mustEnv("S3_BUCKET"),
		APIInternalURL: mustEnv("API_INTERNAL_URL"),
		AIRuntimeURL:   os.Getenv("AI_RUNTIME_URL"),
		AIEnabled:      processor.EnvBool("AI_RUNTIME_ENABLED", false),
	}, sqsClient, s3Client, database)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startTime := time.Now()
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname
	}
	heartbeatInterval := 10
	if v := os.Getenv("WATCHDOG_HEARTBEAT_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			heartbeatInterval = n
		}
	}

	go pollQueueDepth(ctx, sqsClient, queueURL, dlqURL)
	go pollMessages(ctx, sqsClient, queueURL, proc)

	go func() {
		sendHeartbeat := func() {
			hb := db.Heartbeat{
				WorkerID:       workerID,
				TasksProcessed: int64(metrics.GetTasksProcessed()),
				TasksFailed:    int64(metrics.GetTasksFailed()),
				CurrentTaskID:  proc.CurrentTaskID(),
				Goroutines:     runtime.NumGoroutine(),
				UptimeSeconds:  int64(time.Since(startTime).Seconds()),
			}
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := database.UpsertHeartbeat(hbCtx, hb); err != nil {
				slog.Warn("heartbeat upsert failed", "error", err)
			}
			hbCancel()
		}

		sendHeartbeat()

		ticker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendHeartbeat()
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:         ":9091",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("worker metrics server starting", "addr", ":9091")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("worker started",
		"queue_url", queueURL,
		"ai_enabled", processor.EnvBool("AI_RUNTIME_ENABLED", false),
	)
	<-ctx.Done()

	slog.Info("worker shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("metrics server shutdown error", "error", err)
	}
}

func pollMessages(ctx context.Context, client *sqs.Client, queueURL string, proc *processor.Processor) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(queueURL),
			MaxNumberOfMessages:   1,
			WaitTimeSeconds:       20,
			MessageAttributeNames: []string{"traceparent"},
			AttributeNames:        []sqstypes.QueueAttributeName{"ApproximateReceiveCount"},
		})
		metrics.SQSReceiveDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("sqs receive error", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range out.Messages {
			traceparent := ""
			if attr, ok := msg.MessageAttributes["traceparent"]; ok && attr.StringValue != nil {
				traceparent = *attr.StringValue
			}
			receiveCount := 1
			if v, ok := msg.Attributes["ApproximateReceiveCount"]; ok {
				if n, err := strconv.Atoi(v); err == nil {
					receiveCount = n
				}
			}
			slog.Info("received sqs message",
				"message_id", aws.ToString(msg.MessageId),
				"traceparent", traceparent,
				"receive_count", receiveCount,
				"body_len", len(aws.ToString(msg.Body)),
			)
			proc.Process(ctx, aws.ToString(msg.Body), traceparent, aws.ToString(msg.ReceiptHandle), receiveCount)
		}
	}
}

func pollQueueDepth(ctx context.Context, client *sqs.Client, queueURL, dlqURL string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateDepth(ctx, client, queueURL, metrics.QueueDepth.Set, metrics.QueueInflight.Set)
			updateDepth(ctx, client, dlqURL, metrics.DLQDepth.Set, nil)
		}
	}
}

func updateDepth(ctx context.Context, client *sqs.Client, url string, setDepth func(float64), setInflight func(float64)) {
	out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages", "ApproximateNumberOfMessagesNotVisible"},
	})
	if err != nil {
		slog.Warn("failed to get queue attributes", "url", url, "error", err)
		return
	}
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		var n float64
		if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
			setDepth(n)
		}
	}
	if setInflight != nil {
		if v, ok := out.Attributes["ApproximateNumberOfMessagesNotVisible"]; ok {
			var n float64
			if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
				setInflight(n)
			}
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
