.PHONY: build run test vet fmt lint clean coverage release version tag-release

VERSION ?= dev

# Build the agent binary with the current version.
build:
	go build -ldflags "-X main.version=$(VERSION)" -o agent.exe ./cmd/agent

# Print the version used at build time.
version:
	@echo $(VERSION)

# Local release build (requires https://goreleaser.com).
release:
	goreleaser release --snapshot --clean

# Create and push a git tag for a new release.
# Usage: make tag-release VERSION=v1.2.3
tag-release:
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is required. Usage: make tag-release VERSION=v1.2.3"; \
		exit 1; \
	fi
	git tag -a "$(VERSION)" -m "Release $(VERSION)"
	git push origin "$(VERSION)"
	@echo "Tag $(VERSION) pushed. GoReleaser will create the GitHub release automatically."

# Run the agent.
run:
	go run ./cmd/agent

# Run all tests.
test:
	go test ./... -count=1

# Run tests with race detector.
test-race:
	go test ./... -count=1 -race

# Run go vet.
vet:
	go vet ./...

# Format all Go files.
fmt:
	gofmt -w agent auth cmd config history llm markdown structured tools

# Run golangci-lint (requires https://golangci-lint.run).
lint:
	golangci-lint run --timeout=5m

# Show test coverage.
coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

# Remove build artifacts.
clean:
	go clean
	if exist agent.exe del agent.exe
	if exist coverage.out del coverage.out
