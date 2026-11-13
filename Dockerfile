# Build stage pins the Go minor that matches the go directive in go.mod (go 1.23.0).
# Go 1.23 is already past its support window: do not raise the go directive only to
# follow this image. When a derived service needs a newer toolchain, bump go.mod and
# this tag together, and pin both stages by digest
# (FROM golang:1.23-alpine@sha256:... / FROM alpine:3.22@sha256:...) so that rebuilt
# images stay reproducible.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backend-template-go .

# Runtime stage: static binary, non-root user, only the logger config from the repo.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/backend-template-go ./backend-template-go
COPY manifest/config ./manifest/config
RUN mkdir logs && chown -R app:app /app
USER app
# Credentials and the remaining settings come from the runtime environment.
# API_PREFIX mirrors the in-process default and is consumed by the health check below.
ENV APP_ENV=production PORT=3000 HOST=0.0.0.0 API_PREFIX=backend
EXPOSE 3000
# Probe the unauthenticated liveness endpoint. A derived service that removes the
# API prefix must adjust this URL, because an empty prefix yields a doubled slash.
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/${API_PREFIX}/livez" || exit 1
ENTRYPOINT ["./backend-template-go"]
