# Build stage:
# We install everything we need here, build the CSS, generate templ code,
# and compile the app binaries.
FROM golang:1.27.3 AS build
WORKDIR /app

# Keep tool installation independent of application source changes.
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG TAILWIND_VERSION=4.3.3
RUN case "${TARGETARCH:-amd64}" in \
      amd64) tailwind_arch="x64" ;; \
      arm64) tailwind_arch="arm64" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2 && exit 1 ;; \
    esac && \
    wget -q --show-progress -O /usr/local/bin/tailwindcss \
      "https://github.com/tailwindlabs/tailwindcss/releases/download/v${TAILWIND_VERSION}/tailwindcss-linux-${tailwind_arch}" && \
    chmod +x /usr/local/bin/tailwindcss

# Copy Go dependency files first so Docker can cache downloads better.
COPY --link go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Copy the rest of the project files.
COPY --link . .

# Build CSS via the same script CI uses so local/CI/image stay aligned.
# build-css.sh downloads the templui module so its component sources are
# available to Tailwind even on a cold module cache.
RUN --mount=type=cache,target=/go/pkg/mod \
    bash scripts/build-css.sh

# Generate Go files from .templ files.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go tool templ generate

# Build both static Linux binaries for the final image.
#
# The worker is a separate process, not a mode of the API binary: it owns the
# job queue and the periodic sweeps (memory extraction, document indexing,
# embeddings, nudges, reports). One image carries both so a deploy can never
# run a worker built from a different commit than the API.
#
# Goose SQL is embedded via migrations.FS, so the runtime image needs no
# separate migrations directory — `main migrate` applies the schema.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/main ./cmd/web && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/worker ./cmd/worker

# Runtime stage:
# This image stays small and only contains the final app binaries.
FROM alpine:3.20.2
WORKDIR /app

# Install runtime certificates.
RUN apk add --no-cache ca-certificates

# Run the app in production mode.
ENV GO_ENV=production

# Copy the built binaries from the build stage.
COPY --link --from=build /app/main .
COPY --link --from=build /app/worker .

# The app listens on port 8090.
EXPOSE 8090

# ENTRYPOINT rather than CMD so the deployment can pass a subcommand as `args`
# without replacing the binary: the migration hook runs `main migrate`. The
# worker overrides `command` with /app/worker instead.
ENTRYPOINT ["/app/main"]
CMD []
