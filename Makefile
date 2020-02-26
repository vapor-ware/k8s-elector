#
# k8s-elector
#

BIN_NAME    := elector
BIN_VERSION := 1.1.1
IMAGE_NAME  := vaporio/k8s-elector

GIT_COMMIT  ?= $(shell git rev-parse --short HEAD 2> /dev/null || true)
GIT_TAG     ?= $(shell git describe --tags 2> /dev/null || true)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%T 2> /dev/null)
GO_VERSION  := $(shell go version | awk '{ print $$3 }')

LDFLAGS := -w \
	-X main.BuildDate=${BUILD_DATE} \
	-X main.Commit=${GIT_COMMIT} \
	-X main.Tag=${GIT_TAG} \
	-X main.GoVersion=${GO_VERSION} \
	-X main.Version=${BIN_VERSION}

.PHONY: build
build:  ## Build the executable binary
	CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags "${LDFLAGS}" -o ${BIN_NAME} cmd/elector.go

.PHONY: build-linux
build-linux:  # Buld the executable binary for linux amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags "${LDFLAGS}" -o ${BIN_NAME} cmd/elector.go

.PHONY: clean
clean:  ## Remove temporary files and build artifacts
	go clean -v
	rm -rf dist
	rm -f ${BIN_NAME} coverage.out

.PHONY: cover
cover: test  ## Run unit tests and open the coverage report
	go tool cover -html=coverage.out

.PHONY: docker
docker:  ## Build the docker image
	docker build -f Dockerfile \
		-t $(IMAGE_NAME):latest .

.PHONY: fmt
fmt:  ## Run goimports on all go files
	find . -name '*.go' -not -wholename './vendor/*' | while read -r file; do goimports -w "$$file"; done

.PHONY: github-tag
github-tag:  ## Create and push a tag with the current version
	git tag -a ${BIN_VERSION} -m "k8s-elector v${BIN_VERSION}"
	git push -u origin ${BIN_VERSION}

.PHONY: test
test:  ## Run unit tests
	@ # Note: this requires go1.10+ in order to do multi-package coverage reports
	go test -race -coverprofile=coverage.out -covermode=atomic ./pkg/...

.PHONY: version
version:  ## Print the version
	@echo ${BIN_VERSION}

.PHONY: help
help:  ## Print usage information
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

.DEFAULT_GOAL := help
