package tempo

type Span struct {
	SpanID            string            `json:"span_id"`
	ParentSpanID      string            `json:"parent_span_id"`
	OperationName     string            `json:"operation_name"`
	ServiceName       string            `json:"service_name"`
	StartTimeUnixNano int64             `json:"start_time_unix_nano"`
	DurationNano      int64             `json:"duration_nano"`
	Status            string            `json:"status"`
	Attributes        map[string]string `json:"attributes,omitempty"`
}

type TraceResponse struct {
	TraceID  string   `json:"trace_id"`
	Spans    []Span   `json:"spans"`
	Services []string `json:"services"`
}

// OTLP JSON structures returned by Tempo HTTP API.

type otlpResponse struct {
	Batches []otlpBatch `json:"batches"`
}

type otlpBatch struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type otlpScopeSpan struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano any             `json:"startTimeUnixNano"`
	EndTimeUnixNano   any             `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Status            otlpStatus      `json:"status"`
}

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *string `json:"intValue,omitempty"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}
