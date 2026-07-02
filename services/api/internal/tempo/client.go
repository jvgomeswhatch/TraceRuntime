package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

var hexPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) GetTrace(ctx context.Context, traceID string) (*TraceResponse, error) {
	if !hexPattern.MatchString(traceID) {
		return nil, fmt.Errorf("invalid trace ID format")
	}

	url := fmt.Sprintf("%s/api/traces/%s", c.baseURL, traceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tempo request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tempo returned %d", resp.StatusCode)
	}

	var otlp otlpResponse
	if err := json.NewDecoder(resp.Body).Decode(&otlp); err != nil {
		return nil, fmt.Errorf("decode tempo response: %w", err)
	}

	return convertOTLP(traceID, &otlp), nil
}

func ValidTraceID(id string) bool {
	return hexPattern.MatchString(id)
}

func convertOTLP(traceID string, otlp *otlpResponse) *TraceResponse {
	serviceSet := make(map[string]struct{})
	var spans []Span

	for _, batch := range otlp.Batches {
		serviceName := extractServiceName(batch.Resource)
		if serviceName != "" {
			serviceSet[serviceName] = struct{}{}
		}

		for _, scopeSpans := range batch.ScopeSpans {
			for _, s := range scopeSpans.Spans {
				attrs := make(map[string]string, len(s.Attributes))
				for _, a := range s.Attributes {
					attrs[a.Key] = extractValue(a.Value)
				}

				var status string
				switch s.Status.Code {
				case 1:
					status = "ok"
				case 2:
					status = "error"
				default:
					status = "unset"
				}

				spans = append(spans, Span{
					SpanID:            s.SpanID,
					ParentSpanID:      s.ParentSpanID,
					OperationName:     s.Name,
					ServiceName:       serviceName,
					StartTimeUnixNano: parseNano(s.StartTimeUnixNano),
					DurationNano:      parseNano(s.EndTimeUnixNano) - parseNano(s.StartTimeUnixNano),
					Status:            status,
					Attributes:        attrs,
				})
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for s := range serviceSet {
		services = append(services, s)
	}

	return &TraceResponse{
		TraceID:  traceID,
		Spans:    spans,
		Services: services,
	}
}

func extractServiceName(resource otlpResource) string {
	for _, a := range resource.Attributes {
		if a.Key == "service.name" {
			return extractValue(a.Value)
		}
	}
	return ""
}

func extractValue(v otlpValue) string {
	if v.StringValue != nil {
		return *v.StringValue
	}
	if v.IntValue != nil {
		return *v.IntValue
	}
	return ""
}

func parseNano(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		var i int64
		_, _ = fmt.Sscanf(n, "%d", &i)
		return i
	default:
		return 0
	}
}
