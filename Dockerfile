FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o temenos ./cmd/temenos

FROM ghcr.io/tta-lab/organon:latest AS organon

# Download flicknote-cli binary (.tar.xz — requires xz package for tar -J)
# Pulls latest release. Checksum verified against published .sha256 file.
FROM alpine:3.21 AS flicknote-downloader
RUN apk add --no-cache curl xz && \
    ASSET="flicknote-cli-x86_64-unknown-linux-musl.tar.xz" && \
    BASE_URL="https://github.com/GuionAI/flicknote-cli/releases/latest/download" && \
    curl -fsSL "${BASE_URL}/${ASSET}" -o /tmp/flicknote.tar.xz && \
    EXPECTED=$(curl -fsSL "${BASE_URL}/${ASSET}.sha256" | awk '{print $1}') && \
    [ -n "$EXPECTED" ] || { echo "ERROR: sha256 file empty or unavailable" >&2; exit 1; } && \
    echo "${EXPECTED}  /tmp/flicknote.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/flicknote.tar.xz --strip-components=1 -C /tmp \
      "flicknote-cli-x86_64-unknown-linux-musl/flicknote" && \
    [ -f /tmp/flicknote ] || { echo "ERROR: 'note' binary not found after extraction" >&2; exit 1; } && \
    install -m 755 /tmp/flicknote /usr/local/bin/note

# Download taskwarrior binary — asset name varies per version, so GitHub API is used
# to resolve the latest x86_64-linux asset URL dynamically.
# GITHUB_TOKEN avoids 60 req/hr unauthenticated rate limit on shared CI runners.
# Note: GuionAI/taskwarrior releases do not publish .sha256 files — checksum skipped.
FROM alpine:3.21 AS task-downloader
ARG GITHUB_TOKEN
# jq parses GitHub API JSON to locate the release asset URL.
RUN apk add --no-cache curl jq && \
    TASK_URL=$(curl -fsSL \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      "https://api.github.com/repos/GuionAI/taskwarrior/releases/latest" \
      | jq -r '.assets[] | select(.name | test("x86_64-linux")) | .browser_download_url') && \
    [ -n "$TASK_URL" ] && [ "$TASK_URL" != "null" ] || { echo "ERROR: no x86_64-linux asset found" >&2; exit 1; } && \
    curl -fsSL "$TASK_URL" | tar -xzf - -C /tmp && \
    [ -f /tmp/task ] || { echo "ERROR: 'task' binary not found after extraction" >&2; exit 1; } && \
    install -m 755 /tmp/task /usr/local/bin/task

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
COPY --from=flicknote-downloader /usr/local/bin/note /usr/local/bin/note
COPY --from=task-downloader /usr/local/bin/task /usr/local/bin/task

# TCP mode: unauthenticated plain HTTP. Restrict access via Kubernetes NetworkPolicy.
ENV TEMENOS_LISTEN_ADDR=:8081
EXPOSE 8081
# Root required for cgroup v2 memory.max writes (SYS_ADMIN cap from k8s).
# This container has no secrets — all credentials stay in the agent container.
USER root
CMD ["temenos", "daemon"]
