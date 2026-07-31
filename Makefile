GO ?= go
BINARY := bin/x-monitor
VERSION ?= dev
COMMIT ?= unknown
BUILT_AT ?= unknown
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.builtAt=$(BUILT_AT)

.PHONY: build test test-race vet verify sqlc lint vuln clean

build:
	mkdir -p $(dir $(BINARY))
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/x-monitor

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

verify: test test-race vet
	$(GO) mod verify

sqlc:
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

lint:
	golangci-lint run

vuln:
	govulncheck ./...

clean:
	$(GO) clean
	rm -rf ./bin
