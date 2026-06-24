FROM golang:1.21-bookworm AS build
ARG MODULE_DIR=.
WORKDIR /src
# Install build dependencies for CGO (ZeroMQ, pkg-config, toolchain)
RUN apt-get update && apt-get install -y --no-install-recommends build-essential pkg-config libzmq3-dev && rm -rf /var/lib/apt/lists/*
# Copy module files first for better caching
COPY ${MODULE_DIR}/go.mod ${MODULE_DIR}/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
# Copy source
COPY ${MODULE_DIR}/ ./
# Build with CGO enabled (needed for sqlite3 and zmq4)
ENV CGO_ENABLED=1
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /out/indexer ./main.go

# Runtime image with libzmq5 available
FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libzmq5 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/indexer /app/indexer
EXPOSE 8000
ENTRYPOINT ["/app/indexer"]
