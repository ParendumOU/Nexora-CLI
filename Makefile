# NexoraCLI — Go + Bubble Tea terminal client.
# No Go toolchain assumed on the host: everything builds inside the golang container.

GO_IMAGE ?= golang:1.23
DOCKER_RUN = MSYS_NO_PATHCONV=1 docker run --rm -v "$(CURDIR):/app" -w //app $(GO_IMAGE) sh -c

BIN ?= nexora
VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.0.0)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: tidy build run test fmt vet build-all clean

tidy:
	$(DOCKER_RUN) "go mod tidy"

build: tidy
	$(DOCKER_RUN) "go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ."

# Cross-compile for the three host OSes (static, zero-dep binaries).
build-all: tidy
	$(DOCKER_RUN) "\
	  GOOS=linux   GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o dist/$(BIN)-linux-amd64 . && \
	  GOOS=darwin  GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o dist/$(BIN)-darwin-arm64 . && \
	  GOOS=windows GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o dist/$(BIN)-windows-amd64.exe ."

test:
	$(DOCKER_RUN) "go test ./..."

fmt:
	$(DOCKER_RUN) "gofmt -w ."

vet:
	$(DOCKER_RUN) "go vet ./..."

clean:
	rm -rf bin dist
