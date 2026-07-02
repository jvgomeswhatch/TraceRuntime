FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o server ./cmd/server/...

# ---

FROM alpine:3.19 AS runner

# hadolint ignore=DL3018
RUN apk add --no-cache curl && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/server .

USER app

EXPOSE 8082

CMD ["./server"]
