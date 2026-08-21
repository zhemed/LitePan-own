.PHONY: lint lint-install test build build-nofuse web-install web-build web-type-check clean help docker-build docker-up docker-down docker-save

GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo "$(shell go env GOPATH)/bin/golangci-lint")

DOCKER_IMAGE ?= litepan-go:dev
DOCKER_PLATFORM ?= linux/amd64
DOCKER_EXPORT ?= dist/$(DOCKER_IMAGE).tar.gz

lint:
	@GOWORK=off "$(GOLANGCI_LINT)" run -c .golangci.yml ./...

lint-install:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

test:
	@GOWORK=off go test -race ./...

web-install:
	cd web && npm ci

web-build:
	cd web && npm run build

web-type-check:
	cd web && npm run type-check

build: web-build
	@GOWORK=off go build -tags fuse -trimpath -ldflags="-s -w" ./...

build-nofuse:
	@GOWORK=off go build -trimpath -ldflags="-s -w" ./...

clean:
	rm -rf litepan dist/internal/api/web/assets dist/internal/api/web/index.html
	rm -rf web/node_modules

help:
	@echo "make lint          - golangci-lint"
	@echo "make test          - go test -race ./..."
	@echo "make web-build     - vite build (outputs to internal/api/web)"
	@echo "make build         - web-build + go build -tags fuse -trimpath -ldflags"
	@echo "make build-nofuse  - go build without fuse"
	@echo "make docker-build  - docker build (requires ./check-docker-env.sh)"

docker-build:
	@mkdir -p dist
	docker build --platform $(DOCKER_PLATFORM) -t $(DOCKER_IMAGE) .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-save: docker-build
	@mkdir -p dist
	docker save $(DOCKER_IMAGE) | gzip > $(DOCKER_EXPORT)
