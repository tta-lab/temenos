.PHONY: all build test fmt vet lint tidy ci install

BINARY := temenos

all: fmt vet tidy build

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

ci: fmt vet lint test build

run:
	@go run ./cmd/temenos $(ARGS)
