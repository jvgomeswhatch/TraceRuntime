FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN go mod init mock-ai-runtime
COPY main.go .
RUN go build -o /mock-ai-runtime .

FROM alpine:3.20
RUN apk add --no-cache curl
COPY --from=builder /mock-ai-runtime /mock-ai-runtime
EXPOSE 8000
ENTRYPOINT ["/mock-ai-runtime"]
