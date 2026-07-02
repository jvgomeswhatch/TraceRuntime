FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o worker ./cmd/worker

FROM alpine:3.19

# hadolint ignore=DL3018
RUN apk add --no-cache curl && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/worker .

USER app

EXPOSE 9091

CMD ["./worker"]
