PROTO_DIR    := proto/callified/v1
PROTO_OUT    := internal/pb
BINARY       := bin/audiod
IMAGE        := callified-go-audio
IMAGE_TAG    := $(shell git rev-parse --short HEAD 2>/dev/null || echo latest)

.PHONY: all proto proto-python build test clean deps docker docker-push run

all: proto build

## Install Go protoc plugins (run once)
deps:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go mod tidy

## Generate Go gRPC stubs from .proto
proto:
	mkdir -p $(PROTO_OUT)
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc \
		-I proto \
		--go_out=. --go_opt=module=github.com/globussoft/callified-audio \
		--go-grpc_out=. --go-grpc_opt=module=github.com/globussoft/callified-audio \
		$(PROTO_DIR)/audio_pipeline.proto

## Generate Python gRPC stubs (run from repo root)
proto-python:
	python3 -m grpc_tools.protoc \
		-I proto \
		--python_out=.. \
		--grpc_python_out=.. \
		$(PROTO_DIR)/audio_pipeline.proto

## Build the audiod binary
build:
	mkdir -p bin
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/audiod

## Run tests (with race detector)
test:
	go test ./... -v -race -timeout 60s

## Run tests (fast, no race detector — for CI)
test-ci:
	go test ./... -timeout 60s

## Build Docker image
docker:
	docker build -t $(IMAGE):$(IMAGE_TAG) -t $(IMAGE):latest .

## Push Docker image
docker-push: docker
	docker push $(IMAGE):$(IMAGE_TAG)
	docker push $(IMAGE):latest

## Run locally (requires .env in parent directory)
run:
	@if [ -f ../.env ]; then \
		set -a && . ../.env && set +a; \
	fi; \
	GO_AUDIO_PORT=8001 GRPC_ADDR=localhost:50051 ./$(BINARY)

## Clean build artifacts
clean:
	rm -rf bin
