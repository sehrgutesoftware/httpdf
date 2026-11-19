ARG ALPINE_VERSION=3.24
ARG GOLANG_VERSION=1.27.4

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
ENTRYPOINT [ "/sbin/tini", "--", "CompileDaemon" ]
CMD ["-build", "go build -o /build/server ./cmd/server", "-command", "/build/server", "-log-prefix", "false"]

# Install runtime dependencies (same as the production image below)
RUN apk add --no-cache chromium-headless-shell tini \
 && apk info -qL mesa mesa-dri-gallium $(apk info -q | grep -E '^llvm[0-9]+-libs$') \
    | grep . | sed 's|^|/|' | xargs rm -f

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

# The version, e.g. the git tag, is stamped into the binary without a leading v
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION#v}" -o /build/httpdf-server ./cmd/server

# +-----------------------------------------------------------------------------
# | Production Image (doesn't require the source code or golang)
# +-----------------------------------------------------------------------------
FROM alpine:${ALPINE_VERSION}

# Install Chromium's headless shell, and tini (see ENTRYPOINT). GPU rendering is
# disabled (see internal/pdf/renderer.go), so Mesa's gallium driver and LLVM are
# never loaded and can be removed (~200 MB).
RUN apk add --no-cache chromium-headless-shell tini \
 && apk info -qL mesa mesa-dri-gallium $(apk info -q | grep -E '^llvm[0-9]+-libs$') \
    | grep . | sed 's|^|/|' | xargs rm -f

# Create an appuser
RUN adduser -D appuser

USER appuser

# Copy the compiled binaries from the builder image
COPY --from=builder --chown=appuser /build/* /usr/local/bin/

# If launched without setting an explicit entrypoint, we'll run the web server.
# tini runs as PID 1 and reaps the processes Chromium leaves behind, which would
# otherwise pile up as zombies, because httpdf doesn't reap orphaned processes.
ENTRYPOINT [ "/sbin/tini", "--", "httpdf-server" ]
