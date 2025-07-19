package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	pb "hub/trafficmon"

	"slices"

	"github.com/IBM/sarama"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

var (
	messages             []string
	messageLock          sync.Mutex
	latestOffset         int64
	client               sarama.Client
	producer             sarama.AsyncProducer
	websockClients       = make(map[*websocket.Conn]bool)
	websockClientsLock   sync.Mutex
	kafkaMsgChan         = make(chan *sarama.ProducerMessage, 10000)
	kafkaEnabled         bool
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	kafkaBroker    = flag.String("kafka_broker", "", "Kafka broker address")
	kafkaUser      = flag.String("kafka_user", "", "Kafka username")
	kafkaPassword  = flag.String("kafka_password", "", "Kafka password")
	kafkaTopic     = flag.String("kafka_topic", "gotls-events", "Kafka topic")
	kafkaPartition = flag.Int("kafka_partition", 0, "Kafka partition")
	grpcHost       = flag.String("grpc_host", "", "gRPC server host")
	grpcPort       = flag.Int("grpc_port", 50051, "gRPC server port")
	wsHost         = flag.String("ws_host", "", "WebSocket host")
	wsPort         = flag.Int("ws_port", 8085, "WebSocket port")
)

type server struct {
	pb.UnimplementedTrafficCollectorServer
}

var (
	http2Decoder = NewReassembler()
	gzipDecoder  = NewGzipDecompressor()
)

func (s *server) StreamEvents(stream pb.TrafficCollector_StreamEventsServer) error {
	for {
		event, err := stream.Recv()
		if err != nil {
			return err
		}

		event.Uid = generateEventID(event.TsNs, uint64(event.Tid), event.Data)
		event.NormalizedAddrInfo = getNormalizedAddressInfo(event.AddressInfo, event.EventType)
		event.ConnectionId = GetConnectionID(event.NormalizedAddrInfo.SrcIP, event.NormalizedAddrInfo.DstIP,
			int(event.NormalizedAddrInfo.SrcPort), int(event.NormalizedAddrInfo.DstPort))

		frames, err := http2Decoder.Feed(event.ConnectionId, event.Data)
		if err != nil {
			log.Printf("HTTP/2 decode failed: %v", err)
			continue
		}
		for i := range frames {
			if frames[i].Header.Type == "DATA" && !frames[i].Incomplete {
				gzipData, err := gzipDecoder.Feed(event.ConnectionId, frames[i].Header.StreamId, frames[i].Payload, uint8(frames[i].Header.Flags))
				if err == nil && gzipData != "" {
					frames[i].IsGzip = true
					frames[i].GzipData = gzipData
				}
			}
			event.Frames = append(event.Frames, &frames[i])
		}

		bytes, err := json.Marshal(event)
		if err != nil {
			log.Printf("Failed to serialize event: %v", err)
			continue
		}

		messageLock.Lock()
		messages = append(messages, string(bytes))
		messageLock.Unlock()

		if kafkaEnabled {
			select {
			case kafkaMsgChan <- &sarama.ProducerMessage{Topic: *kafkaTopic, Value: sarama.ByteEncoder(bytes)}:
			default:
				log.Println("Kafka message dropped: channel full")
			}
		}

		broadcastToWebsocketClients(string(bytes))
	}
}

func main() {
	flag.Parse()
	grpcAddr := *grpcHost + ":" + strconv.Itoa(*grpcPort)
	wsAddr := *wsHost + ":" + strconv.Itoa(*wsPort)
	kafkaEnabled = *kafkaBroker != ""

	var err error

	if kafkaEnabled {
		config := sarama.NewConfig()
		config.Net.SASL.Enable = true
		config.Net.SASL.User = *kafkaUser
		config.Net.SASL.Password = *kafkaPassword
		config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		config.Net.SASL.Handshake = true
		config.Net.TLS.Enable = false
		config.Net.DialTimeout = 10 * time.Second
		config.Producer.Return.Successes = true
		config.Consumer.Return.Errors = true
		config.Consumer.Offsets.Initial = sarama.OffsetOldest

		client, err = sarama.NewClient([]string{*kafkaBroker}, config)
		if err != nil {
			log.Printf("Kafka disabled: %v", err)
			kafkaEnabled = false
		} else {
			producer, err = sarama.NewAsyncProducerFromClient(client)
			if err != nil {
				log.Printf("Kafka producer error: %v", err)
				kafkaEnabled = false
			} else {
				go func() {
					for err := range producer.Errors() {
						log.Printf("Kafka producer error: %v", err)
					}
				}()
				go func() {
					for range producer.Successes() {}
				}()
				go func() {
					for msg := range kafkaMsgChan {
						producer.Input() <- msg
					}
				}()

				go startKafkaConsumerGroup()
			}
		}
	}

	go func() {
		http.HandleFunc("/ws", websocketHandler)
		log.Printf("WebSocket server started on %s", wsAddr)
		if err := http.ListenAndServe(wsAddr, nil); err != nil {
			log.Fatalf("WebSocket server error: %v", err)
		}
	}()

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatal(err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTrafficCollectorServer(grpcServer, &server{})
	log.Printf("gRPC server listening on %s", grpcAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

func broadcastToWebsocketClients(message string) {
	websockClientsLock.Lock()
	defer websockClientsLock.Unlock()
	for conn := range websockClients {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Printf("WebSocket error: %v", err)
			conn.Close()
			delete(websockClients, conn)
		}
	}
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	websockClientsLock.Lock()
	websockClients[conn] = true
	websockClientsLock.Unlock()

	messageLock.Lock()
	localCopy := slices.Clone(messages)
	messageLock.Unlock()
	for _, msg := range localCopy {
		conn.WriteMessage(websocket.TextMessage, []byte(msg))
	}

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	websockClientsLock.Lock()
	delete(websockClients, conn)
	websockClientsLock.Unlock()
}

type kafkaConsumerGroupHandler struct{}

func (h *kafkaConsumerGroupHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaConsumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaConsumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	log.Printf("Kafka consume claim started for topic %s", claim.Topic())
	for msg := range claim.Messages() {
		payload := string(msg.Value)

		messageLock.Lock()
		messages = append(messages, payload)
		messageLock.Unlock()

		broadcastToWebsocketClients(payload)
		session.MarkMessage(msg, "")
	}
	return nil
}

func startKafkaConsumerGroup() {
	group, err := sarama.NewConsumerGroupFromClient("hub-group-" + strconv.FormatInt(time.Now().UnixNano(), 10), client)
	log.Printf("Kafka consumer group started")
	if err != nil {
		log.Fatalf("Failed to create consumer group: %v", err)
	}
	go func() {
		defer group.Close()
		for {
			if err := group.Consume(context.Background(), []string{*kafkaTopic}, &kafkaConsumerGroupHandler{}); err != nil {
				log.Printf("Kafka consume error: %v", err)
				time.Sleep(2 * time.Second)
			}
		}
	}()
}
