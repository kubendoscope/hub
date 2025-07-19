FROM alpine:latest

# Create a working directory
WORKDIR /app

# Copy the locally built binary into the image
COPY hub .

# Make sure it's executable
RUN chmod +x ./hub

# Expose the gRPC and WebSocket ports
EXPOSE 50051
EXPOSE 8085

# Run the binary
CMD ["./hub"]
