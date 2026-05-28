# syntax=docker/dockerfile:1.7

# --- 1. Build the frontend ---
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --include=dev --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- 2. Build the Go binary with embedded assets ---
FROM golang:1.25-alpine AS api
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/comfynexus ./cmd/comfynexus && mkdir -p /out/data

# --- 3. Minimal runtime ---
# Zeabur rejects some ultra-minimal runtime images during its image-policy check.
# Alpine keeps the image small while still providing a shell for Zeabur runtime tooling.
FROM alpine:3.22
WORKDIR /app
RUN apk add --no-cache ca-certificates su-exec \
    && addgroup -S comfynexus \
    && adduser -S -G comfynexus -h /app -s /sbin/nologin comfynexus \
    && mkdir -p /data \
    && chown -R comfynexus:comfynexus /app /data
COPY --from=api /out/comfynexus /app/comfynexus
COPY --from=api --chown=comfynexus:comfynexus /out/data /data
ENV COMFYNEXUS_DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/bin/sh", "-c", "chown -R comfynexus:comfynexus /data 2>/dev/null || true; exec su-exec comfynexus:comfynexus /app/comfynexus"]
