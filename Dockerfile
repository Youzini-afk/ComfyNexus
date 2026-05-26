# syntax=docker/dockerfile:1.7

# --- 1. Build the frontend ---
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
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
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /out/comfynexus /app/comfynexus
COPY --from=api --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
ENV COMFYNEXUS_DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/app/comfynexus"]
