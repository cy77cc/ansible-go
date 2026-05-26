VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.Commit=$(COMMIT)

.PHONY: build install clean test test-coverage lint fmt vet

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ansible-go ./cmd/ansible-go

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/ansible-go

clean:
	rm -rf bin/ coverage.out

test:
	go test ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-race:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet lint test
