# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /build
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.22-alpine AS backend-builder
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
COPY --from=frontend-builder /build/dist ./static
COPY backend/migrations ./migrations
RUN mkdir -p /app/data
EXPOSE 8080
CMD ["./medvault"]
