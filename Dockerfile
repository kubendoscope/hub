FROM golang:1.24-alpine AS builder

# Create a working directory
WORKDIR /app

# Copy the go source into the image
COPY . .

# Download dependencies
RUN go mod tidy

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o hub .

# Stage 2: Run the Go app in Alpine
FROM alpine:latest

# Copy from BUILDER
COPY --from=builder /app/hub .

# Make sure it's executable
RUN chmod +x ./hub

# Expose the gRPC and WebSocket ports
EXPOSE 50051
EXPOSE 8085

# Run the binary
CMD ["./hub"]
