ARG ALPINE_VERSION=3.20
ARG GOLANG_VERSION=1.23

# +-----------------------------------------------------------------------------
# | Vendor Dependencies
# +-----------------------------------------------------------------------------
FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS vendor

WORKDIR /app

# Dependencies are installed in a separate layer, so they don't need to be
# reinstalled each time the source code changes.
COPY go.mod go.sum ./
RUN go mod download

# +-----------------------------------------------------------------------------
# | Hot-reloading Target for Development
# +-----------------------------------------------------------------------------
FROM vendor AS hot-reload

RUN mkdir /build
ENTRYPOINT [ "CompileDaemon" ]
CMD ["-build", "go build -o /build/server ./cmd/server", "-command", "/build/server", "-log-prefix", "false"]

# Install runtime dependencies
RUN apk add --no-cache chromium

# Install CompileDaemon, which will watch for changes in the source code and
# recompile the binaries on the fly.
RUN apk add --no-cache git &&\
    go get github.com/sehrgutesoftware/CompileDaemon@78c20d1 &&\
    go install github.com/sehrgutesoftware/CompileDaemon

# +-----------------------------------------------------------------------------
# | Binaries Builder
# +-----------------------------------------------------------------------------
FROM vendor AS builder

# Build all binaries and make them available in the /build directory. The final
# image layer will copy them from there.
COPY . .
RUN go build -o /build/httpdf-server ./cmd/server

# +-----------------------------------------------------------------------------
# | Production Image (doesn't require the source code or golang)
# +-----------------------------------------------------------------------------
FROM alpine:${ALPINE_VERSION}

# Install necessary packages, including Chromium
RUN apk add --no-cache chromium

# Create an appuser
RUN adduser -D appuser

USER appuser

# Copy the compiled binaries from the builder image
COPY --from=builder --chown=appuser /build/* /usr/local/bin/

# If launched without setting an explicit entrypoint, we'll run the web server
ENTRYPOINT [ "httpdf-server" ]
