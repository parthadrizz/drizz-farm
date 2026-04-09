VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
BINARY  := drizz-farm

LDFLAGS := -s -w \
	-X github.com/drizz-dev/drizz-farm/cmd.Version=$(VERSION) \
	-X github.com/drizz-dev/drizz-farm/cmd.Commit=$(COMMIT) \
	-X github.com/drizz-dev/drizz-farm/cmd.BuildDate=$(DATE)

.PHONY: build test lint run clean fmt vet

## build: Compile the binary
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

## run: Build and run the daemon
run: build
	./bin/$(BINARY) start

## test: Run unit tests
test:
	go test -race -count=1 ./...

## test-integration: Run integration tests (requires Android SDK)
test-integration:
	go test -race -count=1 -tags=integration ./...

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## fmt: Format code
fmt:
	go fmt ./...
	goimports -w .

## vet: Run go vet
vet:
	go vet ./...

## clean: Remove build artifacts
clean:
	rm -rf bin/

## tidy: Tidy go modules
tidy:
	go mod tidy

## all: Format, vet, test, build
all: fmt vet test build
