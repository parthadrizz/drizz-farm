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

## test-integration: Run the full E2E / integration suite.
## Boots the daemon, runs every test file under ./tests/ against the
## live API, shuts down. A real Android emulator must be reachable
## via ADB for the device-simulation sub-tests; any test requiring a
## warm device skips cleanly if no emulator is up.
test-integration:
	go test -race -count=1 -tags=integration -timeout 10m -v ./tests/

## test-capabilities: Run ONLY the capability-conformance suite.
## Tighter 5-minute budget, focused on the v0.1.14 → v0.1.21 surface
## (sessions, devices, reservations, artifacts, captures, multipart
## upload, camera inject, device-sim with on-device verification).
## Prints a pass/fail summary at the end.
test-capabilities:
	go test -count=1 -tags=integration -timeout 5m -v -run TestCapabilities_FullSuite ./tests/

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

# ── Release targets ──────────────────────────────────────────────────────

## release-mac: Build a universal macOS binary (arm64 + amd64)
release-mac: build-dashboard
	@mkdir -p bin
	@echo "→ Building darwin/arm64..."
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 .
	@echo "→ Building darwin/amd64..."
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-amd64 .
	@echo "→ Combining with lipo..."
	lipo -create -output bin/$(BINARY)-darwin-universal bin/$(BINARY)-darwin-arm64 bin/$(BINARY)-darwin-amd64
	@file bin/$(BINARY)-darwin-universal
	@echo "✓ Universal binary: bin/$(BINARY)-darwin-universal"

## release-linux: Build Linux binaries (arm64 + amd64) — experimental
release-linux: build-dashboard
	@mkdir -p bin
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 .
	@echo "✓ Linux binaries: bin/$(BINARY)-linux-{arm64,amd64}"

## release: Build all macOS release artifacts + tarballs with checksums
release: release-mac
	@mkdir -p dist
	@cd bin && \
		tar czf ../dist/$(BINARY)-$(VERSION)-darwin-universal.tar.gz $(BINARY)-darwin-universal && \
		tar czf ../dist/$(BINARY)-$(VERSION)-darwin-arm64.tar.gz $(BINARY)-darwin-arm64 && \
		tar czf ../dist/$(BINARY)-$(VERSION)-darwin-amd64.tar.gz $(BINARY)-darwin-amd64
	@cd dist && shasum -a 256 *.tar.gz > SHA256SUMS
	@echo ""
	@echo "✓ Release artifacts in dist/:"
	@ls -lh dist/
	@echo ""
	@echo "SHA256:"
	@cat dist/SHA256SUMS
