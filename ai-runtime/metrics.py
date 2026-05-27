from prometheus_client import Counter, Histogram, Gauge

ai_requests_total = Counter(
    "ai_requests_total",
    "Total inference requests",
    ["execution_status", "task_type"],
)

ai_request_duration_seconds = Histogram(
    "ai_request_duration_seconds",
    "Total request duration including graph execution",
    ["task_type"],
    buckets=[0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0],
)

ai_inference_duration_seconds = Histogram(
    "ai_inference_duration_seconds",
    "Ollama inference duration",
    ["model"],
    buckets=[0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0],
)

ai_inference_in_flight = Gauge(
    "ai_inference_in_flight",
    "Number of Ollama inference calls currently in progress",
)

ai_validation_status_total = Counter(
    "ai_validation_status_total",
    "Output validation outcomes",
    ["validation_status"],
)

ai_failures_total = Counter(
    "ai_failures_total",
    "Inference failures by reason",
    ["reason"],
)

ai_output_chars = Histogram(
    "ai_output_chars",
    "Output size in characters",
    ["model", "truncated"],
    buckets=[100, 500, 1000, 2000, 4000, 8000],
)
