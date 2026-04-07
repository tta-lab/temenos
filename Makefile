.PHONY: all build test fmt vet lint tidy ci install install-hooks

BINARY := temenos

all: fmt tidy lint build

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

ci: fmt tidy lint test build
	@echo "✓ CI checks complete"

# Install lefthook git hooks (pre-commit: gofmt + goimports, pre-push: golangci-lint + trufflehog)
install-hooks:
	@lefthook install
	@echo "✓ Lefthook hooks installed (pre-commit: gofmt + goimports, pre-push: golangci-lint + trufflehog)"
