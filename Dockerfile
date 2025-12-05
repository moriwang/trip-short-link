# Build stage
# Use the host platform for building to maximize speed (avoiding QEMU for compilation)
FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS builder

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./

# Arguments automatically provided by Docker Buildx
ARG TARGETOS
ARG TARGETARCH

# Build the application with optimization flags
# We explicitly use the target OS and architecture for cross-compilation
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -a -installsuffix cgo -ldflags="-s -w" -o proxy-server .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/proxy-server .

# Expose port (will be overridden by docker-compose)
EXPOSE 80

# Run the application
CMD ["./proxy-server"]