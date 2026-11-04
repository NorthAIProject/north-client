# Build stage:
# We download the modules and compile the app binaries.
FROM golang:1.27.3 AS build
WORKDIR /app

# Copy Go dependency files first so Docker can cache downloads better.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the project files.
COPY . .

# Build both static Linux binaries for the final image.
#
# The worker is a separate process, not a mode of the API binary: it owns the
# job queue and the periodic sweeps (memory extraction, document indexing,
# embeddings, nudges, reports). One image carries both so a deploy can never
# run a worker built from a different commit than the API.
#
# Goose SQL is embedded via migrations.FS, so the runtime image needs no
# separate migrations directory — `main migrate` applies the schema.
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/api
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
