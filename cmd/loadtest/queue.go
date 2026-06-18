package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type QueueMetrics struct {
	MaxVisible  int64
	MaxInflight int64
	mu          sync.Mutex
}

func (qm *QueueMetrics) update(visible, inflight int64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	if visible > qm.MaxVisible {
		qm.MaxVisible = visible
	}
	if inflight > qm.MaxInflight {
		qm.MaxInflight = inflight
	}
}

func (qm *QueueMetrics) snapshot() (maxVisible, maxInflight int64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.MaxVisible, qm.MaxInflight
}

// newSQSClient creates an SQS client pointing at LocalStack,
// deriving the endpoint from the queue URL (e.g. http://localhost:4566/...).
func newSQSClient(ctx context.Context, sqsURL string) (*sqs.Client, error) {
	parsed, err := url.Parse(sqsURL)
	if err != nil {
		return nil, fmt.Errorf("parse sqs url: %w", err)
	}
	endpoint := fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
	return client, nil
}

func trackQueueMetrics(ctx context.Context, sqsURL string, qm *QueueMetrics) {
	client, err := newSQSClient(ctx, sqsURL)
	if err != nil {
		slog.Warn("queue tracker: failed to create SQS client", "error", err)
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl: aws.String(sqsURL),
				AttributeNames: []sqstypes.QueueAttributeName{
					"ApproximateNumberOfMessages",
					"ApproximateNumberOfMessagesNotVisible",
				},
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("queue tracker: get attributes failed", "error", err)
				continue
			}

			var visible, inflight int64
			if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
				fmt.Sscanf(string(v), "%d", &visible)
			}
			if v, ok := out.Attributes["ApproximateNumberOfMessagesNotVisible"]; ok {
				fmt.Sscanf(string(v), "%d", &inflight)
			}
			qm.update(visible, inflight)
		}
	}
}

func checkBacklogConverged(ctx context.Context, sqsURL string) bool {
	client, err := newSQSClient(ctx, sqsURL)
	if err != nil {
		return false
	}

	out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(sqsURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			"ApproximateNumberOfMessages",
		},
	})
	if err != nil {
		return false
	}

	var visible int64
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		fmt.Sscanf(string(v), "%d", &visible)
	}
	return visible == 0
}
