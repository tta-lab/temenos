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
    echo "${EXPECTED}  /tmp/flicknote.tar.xz" | sha256sum -c - && \
    tar -xJf /tmp/flicknote.tar.xz --strip-components=1 -C /tmp \
      "flicknote-cli-x86_64-unknown-linux-musl/flicknote" && \
    install -m 755 /tmp/flicknote /usr/local/bin/note

# Download taskwarrior binary (.tar.gz — gzip handled natively by busybox tar)
# Pulls latest release via GitHub API. GITHUB_TOKEN avoids 60 req/hr unauthenticated limit.
# Note: GuionAI/taskwarrior releases do not publish .sha256 files — checksum skipped.
FROM alpine:3.21 AS task-downloader
ARG GITHUB_TOKEN
RUN apk add --no-cache curl jq && \
    TASK_URL=$(curl -fsSL \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      "https://api.github.com/repos/GuionAI/taskwarrior/releases/latest" \
      | jq -r '.assets[] | select(.name | test("x86_64-linux")) | .browser_download_url') && \
    [ -n "$TASK_URL" ] && [ "$TASK_URL" != "null" ] || { echo "ERROR: no x86_64-linux asset found" >&2; exit 1; } && \
    curl -fsSL "$TASK_URL" | tar -xzf - -C /tmp && \
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
