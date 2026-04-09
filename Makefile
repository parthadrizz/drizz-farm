VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
BINARY  := drizz-farm

LDFLAGS := -s -w \
	-X github.com/drizz-dev/drizz-farm/internal/buildinfo.Version=$(VERSION) \
	-X github.com/drizz-dev/drizz-farm/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/drizz-dev/drizz-farm/internal/buildinfo.BuildDate=$(DATE)

.PHONY: build test lint run clean fmt vet

## build: Build React dashboard + Go binary (single distributable)
build: build-dashboard
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) github.com/drizz-dev/drizz-farm

## build-go: Build Go binary only (skip dashboard rebuild)
build-go:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) github.com/drizz-dev/drizz-farm

## build-dashboard: Build React dashboard and copy to embed directory
build-dashboard:
	cd web && npm run build
	rm -rf internal/api/dashboard
	cp -r web/dist internal/api/dashboard

## run: Build and run the daemon
run: build
	./bin/$(BINARY) start

## test: Run unit tests
test:
	go test -race -count=1 ./...

## test-integration: Run integration tests (requires Android SDK + AVDs created)
## Starts daemon, runs tests against live API, shuts down
test-integration:
	go test -race -count=1 -tags=integration -timeout 10m -v ./tests/

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
