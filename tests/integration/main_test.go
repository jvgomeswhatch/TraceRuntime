package integration

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) {
	apiURL = envOr("API_URL", "http://localhost:8082")
	queueURL = envOr("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks")
	dlqURL = envOr("SQS_DLQ_URL", "http://localhost:4566/000000000000/traceruntime-tasks-dlq")
	sqsEndpoint := envOr("SQS_ENDPOINT", "http://localhost:4566")
	s3Endpoint := envOr("S3_ENDPOINT", "http://localhost:4566")
	dbURL := envOr("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable")

	ctx := context.Background()

	// Connect to PostgreSQL.
	var err error
	dbPool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	defer dbPool.Close()

	// Build AWS SDK config pointing at LocalStack.
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		log.Fatalf("cannot load AWS config: %v", err)
	}

	sqsClient = sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		o.BaseEndpoint = &sqsEndpoint
	})

	s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &s3Endpoint
		o.UsePathStyle = true
	})

	// Wait for the API to be ready before running any tests.
	if err := waitForService(apiURL+"/health", 30*time.Second); err != nil {
		log.Fatalf("API not ready: %v", err)
	}

	os.Exit(m.Run())
}

// waitForService polls a health endpoint until it returns 200 or the timeout expires.
func waitForService(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("service at %s not ready after %v", url, timeout)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
