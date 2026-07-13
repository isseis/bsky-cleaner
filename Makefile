.PHONY: fmt test lint build clean deadcode notify-preview notify-preview-send bump-version

BINARY=build/bsky-cleaner

build:
	go build -o $(BINARY) ./cmd

clean:
	go clean
	rm -f $(BINARY)

fmt:
	gofmt -l -w .

test:
	go test -tags test ./...

lint:
	golangci-lint run

deadcode:
	deadcode ./...

notify-preview:
	go run -tags test ./internal/notify/notifypreview $(ARGS)

notify-preview-send:
	go run -tags test ./internal/notify/notifypreview -send $(ARGS)

bump-version:
	go run ./scripts/bump_release_version $(ARGS)
