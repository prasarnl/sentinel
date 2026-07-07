# Build context is expected to be the repository root (see ../deploy/docker-compose.yml),
# since this image bundles agent binaries built from ../agent alongside the server.

FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS agent-builder
WORKDIR /app/agent
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN mkdir -p /out && \
    GOOS=linux   GOARCH=amd64 go build -o /out/sentinel-agent-linux-amd64 . && \
    GOOS=linux   GOARCH=arm64 go build -o /out/sentinel-agent-linux-arm64 . && \
    GOOS=windows GOARCH=amd64 go build -o /out/sentinel-agent-windows-amd64.exe .

FROM golang:1.26-alpine AS server-builder
WORKDIR /app/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web-builder /app/web/dist ./internal/spa/dist
RUN go build -o /out/sentinel-server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=server-builder /out/sentinel-server ./sentinel-server
COPY --from=agent-builder /out/ ./downloads/
ENV DOWNLOADS_DIR=/app/downloads
EXPOSE 4000
ENTRYPOINT ["./sentinel-server"]
