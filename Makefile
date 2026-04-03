.PHONY: all build test fmt vet lint tidy ci install install-hooks qlty

BINARY := temenos

all: fmt tidy qlty build

build:
	@echo "Building $(BINARY)..."
	@go build -o $(BINARY) ./cmd/temenos

install:
	@go install ./cmd/temenos

test:
	@go test -v ./...

fmt:
	@gofmt -w -s .

vet:
	@go vet ./...

lint:
	@golangci-lint run ./...

tidy:
	@go mod tidy

qlty:
	@echo "Running qlty check..."
	@qlty check --all --no-progress
	@echo "✓ Qlty check complete"

install-hooks:
	@qlty githooks install

ci: fmt tidy qlty test build
