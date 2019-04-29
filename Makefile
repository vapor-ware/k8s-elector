#
# k8s-elector
#

BIN_NAME    := elector
BIN_VERSION := 0.0.1
IMAGE_NAME  := vaporio/k8s-elector

.PHONY: build
build:  ## Build the executable binary
	CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags '-w' -o ${BIN_NAME} cmd/elector.go

.PHONY: clean
clean:  ## Remove temporary files and build artifacts
	go clean -v
	rm -rf dist
	rm -f ${BIN_NAME}

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

.PHONY: version
version:  ## Print the version
	@echo ${BIN_VERSION}

.PHONY: help
help:  ## Print usage information
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

.DEFAULT_GOAL := help