.PHONY: fmt test lint build clean deadcode

BINARY=build/bsky-cleaner

build:
	go build -o $(BINARY) ./cmd

clean:
	go clean
	rm -f $(BINARY)

fmt:
	gofmt -l -w .

test:
	go test ./...

lint:
	golangci-lint run

deadcode:
	deadcode ./...
