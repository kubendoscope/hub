# hub

`hub` is the central ingestion service for `tracer` events.  
It accepts streamed Go TLS events over gRPC, enriches them with connection-level metadata, decodes HTTP/2 frames, optionally decompresses gzip payloads, and publishes the resulting JSON to WebSocket clients and optionally Kafka.

## What it does

- Runs a gRPC server that implements `traffic.TrafficCollector/StreamEvents`.
- Accepts a continuous stream of `GoTlsEvent` messages from tracer agents.
- Enriches each event with:
  - deterministic event ID (`uid`)
  - normalized source/destination address info
  - stable connection ID
- Reassembles and decodes HTTP/2 frames per connection.
- Attempts gzip decompression for completed HTTP/2 `DATA` frames.
- Broadcasts processed events to all connected WebSocket clients on `/ws`.
- Optionally writes events to Kafka and consumes from Kafka for rebroadcast.

## Event pipeline

1. Receive `GoTlsEvent` from gRPC stream.
2. Normalize address direction using event type.
3. Compute `connection_id` and `uid`.
4. Parse/reassemble HTTP/2 frames from raw payload bytes.
5. For complete DATA frames, try gzip stream reconstruction and decompression.
6. Attach decoded frames to event.
7. Serialize event to JSON and:
   - store in memory (`messages`)
   - broadcast to WebSocket clients
   - enqueue to Kafka (if enabled)

## Interfaces

- gRPC ingest: `-grpc_host` + `-grpc_port` (default port `50051`)
- WebSocket broadcast endpoint: `ws://<ws_host>:<ws_port>/ws` (default port `8085`)
- Kafka (optional): producer + consumer-group relay for the configured topic

## Configuration flags

- `-grpc_host` (default: empty, binds on all interfaces when used as `:50051`)
- `-grpc_port` (default: `50051`)
- `-ws_host` (default: empty, binds on all interfaces when used as `:8085`)
- `-ws_port` (default: `8085`)
- `-kafka_broker` (default: empty, disables Kafka when not provided)
- `-kafka_user` (default: empty)
- `-kafka_password` (default: empty)
- `-kafka_topic` (default: `gotls-events`)
- `-kafka_partition` (default: `0`)

## Run locally

```bash
go mod tidy
go run .
```

Example with explicit bind addresses:

```bash
go run . \
  -grpc_host 0.0.0.0 -grpc_port 50051 \
  -ws_host 0.0.0.0 -ws_port 8085
```

Example with Kafka enabled:

```bash
go run . \
  -kafka_broker <broker:port> \
  -kafka_user <user> \
  -kafka_password <password> \
  -kafka_topic gotls-events
```

## Build binary

```bash
go build -o hub
./hub -grpc_host 0.0.0.0 -ws_host 0.0.0.0
```

## Docker

Build:

```bash
docker build -t hub:local .
```

Run:

```bash
docker run --rm -p 50051:50051 -p 8085:8085 hub:local
```

The image uses a multi-stage build and runs a static `hub` binary in Alpine.

## Protobuf contract

Defined at `proto/traffic-mon.proto`.

- Service: `TrafficCollector`
- Method: `StreamEvents(stream GoTlsEvent) returns (StreamResponse)`
- `GoTlsEvent` includes raw transport fields from tracer plus hub-enriched fields:
  - `normalized_addr_info`
  - `connection_id`
  - `frames` (decoded HTTP/2 frame metadata and payload)

## Notes

- WebSocket clients receive all retained messages first, then live updates.
- If Kafka is not configured or fails to initialize, hub continues operating with gRPC + WebSocket.
- HTTP/2 parsing is stateful per connection and supports fragmented frame reassembly.

## Repository layout

- `main.go`: gRPC ingest server, WebSocket server, Kafka integration
- `http2_parser.go`: frame reassembly and HTTP/2 metadata extraction
- `gzip_decompressor.go`: gzip stream accumulation/decompression for DATA frames
- `utils.go`: normalization helpers, IDs, connection keying
- `proto/traffic-mon.proto`: protobuf service and message definitions
