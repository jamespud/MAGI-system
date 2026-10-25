# =============================================================================
# MAGI backend (Go) - per-component Makefile
# Invoked by the root Makefile: make -C backend <target>
# =============================================================================

GO ?= go
BIN := ../bin/magi

.PHONY: install build run m1-e2e test test-race fmt vet tidy lint clean

install:
	$(GO) mod download

build:
	@mkdir -p ../bin
	$(GO) build -o $(BIN) ./cmd/magi-server

run:
	$(GO) run ./cmd/magi-server

m1-e2e:
	$(GO) run ./cmd/m1-e2e -mode $${M1_MODE:-real}

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

lint:
	$(GO) fmt ./...
	$(GO) vet ./...

clean:
	rm -f $(BIN)
