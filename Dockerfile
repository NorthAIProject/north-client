# Build stage:
# We install everything we need here, build the CSS, generate templ code,
# and compile the app binary.
FROM golang:1.27 AS build
WORKDIR /app

# Copy Go dependency files first so Docker can cache downloads better.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the project files.
COPY . .

# Install tools needed during the build.
RUN apt-get update && apt-get install -y wget ca-certificates && rm -rf /var/lib/apt/lists/*

# Download the Tailwind standalone binary for the current CPU architecture.
RUN arch="$(dpkg --print-architecture)" && \
    case "$arch" in \
      amd64) tailwind_arch="x64" ;; \
      arm64) tailwind_arch="arm64" ;; \
      *) echo "unsupported architecture: $arch" && exit 1 ;; \
    esac && \
    wget -O /usr/local/bin/tailwindcss "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-${tailwind_arch}" && \
    chmod +x /usr/local/bin/tailwindcss

# Build CSS via the same script CI uses so local/CI/image stay aligned.
# build-css.sh downloads the templui module so its component sources are
# available to Tailwind even on a cold module cache.
RUN bash scripts/build-css.sh

# Generate Go files from .templ files.
RUN go tool templ generate

# Build both static Linux binaries for the final image.
#
# The worker is a separate process, not a mode of the web binary: it owns the
# job queue and the periodic sweeps (memory extraction, document indexing,
# embeddings, nudges, reports). One image carries both so a deploy can never
# run a worker built from a different commit than the web app.
#
# Goose SQL is embedded via migrations.FS, so the runtime image needs no
# separate migrations directory — `main migrate` applies the schema.
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/web
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker

# Runtime stage:
# This image stays small and only contains the final app binaries.
FROM alpine:3.20.2
WORKDIR /app

# Install runtime certificates.
RUN apk add --no-cache ca-certificates

# Run the app in production mode.
ENV GO_ENV=production

# Copy the built binaries from the build stage.
COPY --from=build /app/main .
COPY --from=build /app/worker .

# The app listens on port 8090.
EXPOSE 8090

# ENTRYPOINT rather than CMD so the deployment can pass a subcommand as `args`
# without replacing the binary: the migration hook runs `main migrate`. The
# worker overrides `command` with /app/worker instead.
ENTRYPOINT ["/app/main"]
CMD []
