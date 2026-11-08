BINARY   := weather-otel
MODULE   := github.com/eftechcombr/weather
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE    ?= weather-otel:$(VERSION)
GOFLAGS  ?=

.PHONY: all build test race vet fmt check run atlas docker clean

all: check build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -trimpath \
		-ldflags "-X main.version=$(VERSION)" \
		-o bin/$(BINARY) ./cmd/weather-otel

test:
	go test $(GOFLAGS) ./...

race:
	go test $(GOFLAGS) -race ./...

vet:
	go vet $(GOFLAGS) ./...

fmt:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt vet test

run:
	go run ./cmd/weather-otel

atlas:
	go run ./cmd/atlasgen

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

clean:
	rm -rf bin
