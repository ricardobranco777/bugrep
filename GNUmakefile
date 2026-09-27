MODULE     := github.com/ricardobranco777/bugrep
VERSION    := $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS    := -X '$(MODULE)/internal/cli.Version=$(VERSION)'
BIN        := bugrep
GOBIN      := $(shell go env GOPATH)/bin

.PHONY: all
all: build

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/bugrep

.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/bugrep

.PHONY: run
run: build
	./$(BIN) $(ARGS)

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race -count=1 ./...

.PHONY: cover
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "The following files are not gofmt'd:"; \
		echo "$$out"; \
		exit 1; \
	fi

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: check
check: fmt-check vet lint test-race

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -f $(BIN) coverage.out

.PHONY: completions
completions: build
	mkdir -p completions
	./$(BIN) completion bash > completions/$(BIN).bash
	./$(BIN) completion zsh  > completions/$(BIN).zsh
	./$(BIN) completion fish > completions/$(BIN).fish
