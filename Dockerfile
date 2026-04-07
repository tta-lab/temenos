FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o temenos ./cmd/temenos

FROM ghcr.io/tta-lab/organon:latest AS organon

FROM alpine:3.21
RUN apk add --no-cache \
    bubblewrap \
    bash \
    curl \
    sed \
    coreutils \
    ripgrep \
    python3 \
    jq

COPY --from=builder /app/temenos /usr/local/bin/temenos
COPY --from=organon /usr/local/bin/src /usr/local/bin/web /usr/local/bin/
COPY flicknote /usr/local/bin/note
COPY task /usr/local/bin/task

# TCP mode: unauthenticated plain HTTP. Restrict access via Kubernetes NetworkPolicy.
ENV TEMENOS_LISTEN_ADDR=:8081
EXPOSE 8081
# Root required for cgroup v2 memory.max writes (SYS_ADMIN cap from k8s).
# This container has no secrets — all credentials stay in the agent container.
USER root
CMD ["temenos", "daemon"]
