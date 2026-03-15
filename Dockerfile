FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o temenos ./cmd/temenos

FROM alpine:3.21
RUN apk add --no-cache \
    bubblewrap \
    bash \
    curl \
    sed \
    coreutils \
    ripgrep

COPY --from=builder /app/temenos /usr/local/bin/temenos

ENV TEMENOS_LISTEN_ADDR=:8081
EXPOSE 8081
USER nobody
CMD ["temenos", "daemon"]
