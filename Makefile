
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


download-tools:
	go install golang.org/x/tools/cmd/goimports@latest

install-go:
	go mod download

format:
	gofmt -w -s .
	goimports -w .

test: format generate
	go test ./...

test-race: format generate
	go test -race ./... -v

test-verbose: format generate
	go test ./... -v

test-clean: format generate
	go clean -testcache


.PHONY: release
release:
	docker build -t mitosu:build -f docker/Dockerfile.build docker
	docker run \
		--rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $$PWD:/work \
		-w /work \
		mitosu:build \
		release --skip=validate --clean --snapshot


.PHONY: release-github
release-github:
	docker build -t mitosu:build -f docker/Dockerfile.build docker
	docker run \
		--rm \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $$PWD:/work \
		-e GITHUB_TOKEN=$$GITHUB_TOKEN \
		-w /work \
		mitosu:build \
		release --skip=validate --clean

