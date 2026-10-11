# Stage 1: Build frontend
# vite.config.ts writes the build to ../backend/static (so dev `go run .`
# can serve it directly). In this stage, that resolves to /backend/static.
FROM node:24-alpine AS frontend-builder
WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build
# After this step the output lives at /build/backend/static

# Stage 2: Build backend
FROM golang:1.27-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -o medvault .
# Drug autofill DBs (data/<iso>/drugs.db), if present in the checkout.
RUN mkdir -p data

# Stage 3: Minimal runtime
FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /build/medvault .
COPY --from=frontend-builder /build/backend/static ./static
COPY --from=backend-builder /build/data ./drugs
COPY backend/migrations ./migrations
RUN mkdir -p /app/data
# Drug DBs live outside the /app/data volume so image updates replace them.
ENV DRUG_DB_DIR=/app/drugs
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/api/ready" >/dev/null || exit 1
CMD ["./medvault"]
