
VERSION := $(shell git describe --tags --abbrev=0)
COMMIT  := $(shell git rev-parse --verify HEAD)
DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w
LDFLAGS += -X main.BuildMode=prod
LDFLAGS += -X main.BuildDate=$(DATE)
LDFLAGS += -X main.BuildCommit=$(COMMIT)
LDFLAGS += -X main.BuildVersion=$(VERSION)

TAGS    := netgo osusergo

SITE_SRC := ./main.go
SITE_DST := ./main.o


# The web UI, built with npm inside Docker (scripts/web.sh), embedded by
# the Go build.
.PHONY: web
web:
	scripts/web.sh

.PHONY: build
build: web
	CGO_ENABLED=0 go build -trimpath -o mitosu .

download-tools:
	go install golang.org/x/tools/cmd/goimports@latest

install-go:
	go mod download

format:
	gofmt -w -s .
	goimports -w .

test: format web
	go test ./...

test-race: format web
	go test -race ./... -v

test-verbose: format web
	go test ./... -v

test-clean: format web
	go clean -testcache

