# Build stage: compile bsky-cleaner and fetch supercronic.
# The digest is pinned for reproducibility (AC-06).
FROM golang@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/bsky-cleaner ./cmd

# Fetch supercronic via go install so the Go module checksum database
# verifies the source (supply-chain protection, see architecture doc 3.2.2).
RUN go install github.com/aptible/supercronic@v0.2.47

# Runtime stage: minimal Alpine image with the binary, supercronic, and
# entrypoint script. Run as a non-root user (UID 10001) for defence in depth
# (architecture doc 5.3).
FROM alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates && adduser -D -u 10001 bsky

COPY --from=build /out/bsky-cleaner /usr/local/bin/bsky-cleaner
COPY --from=build /go/bin/supercronic /usr/local/bin/supercronic
COPY --chmod=0755 entrypoint.sh /entrypoint.sh

USER bsky
ENTRYPOINT ["/entrypoint.sh"]
