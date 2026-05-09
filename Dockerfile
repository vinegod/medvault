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
FROM golang:1.25-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -o medvault .

# Stage 3: Minimal runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend-builder /build/medvault .
COPY --from=frontend-builder /build/backend/static ./static
COPY backend/migrations ./migrations
RUN mkdir -p /app/data
EXPOSE 8080
CMD ["./medvault"]
