package http

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
)

type MetricsHandler struct {
	broker  *event.Broker
	queue   *queue.Queue
	handler http.Handler
}

func NewMetricsHandler(broker *event.Broker, q *queue.Queue) *MetricsHandler {
	return &MetricsHandler{
		broker:  broker,
		queue:   q,
		handler: promhttp.Handler(),
	}
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}
