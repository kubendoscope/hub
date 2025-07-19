// package main

// import (
// 	"bytes"
// 	"encoding/base64"
// 	"encoding/binary"
// 	"fmt"
// 	"sync"
// )

// var (
// 	HTTP2Preface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
// 	HTTP2FrameTypes = map[uint8]string{
// 		0x00: "DATA",
// 		0x01: "HEADERS",
// 		0x02: "PRIORITY",
// 		0x03: "RST_STREAM",
// 		0x04: "SETTINGS",
// 		0x05: "PUSH_PROMISE",
// 		0x06: "PING",
// 		0x07: "GOAWAY",
// 		0x08: "WINDOW_UPDATE",
// 		0x09: "CONTINUATION",
// 	}
// )

// type FrameHeader struct {
// 	Length   int
// 	Type     string
// 	Flags    uint8
// 	StreamID uint32
// }

// type ConnectionState struct {
// 	Buffer    []byte
// 	Expecting *FrameHeader
// }

// type Reassembler struct {
// 	states sync.Map // map[string]*ConnectionState
// }

// func NewReassembler() *Reassembler {
// 	return &Reassembler{}
// }

// type DecodedFrame struct {
// 	ConnectionID   string
// 	Header         *FrameHeader
// 	Payload        []byte
// 	Incomplete     bool
// 	AdditionalInfo string
// }

// func (r *Reassembler) Feed(connectionID string, base64Data string) ([]DecodedFrame, error) {
// 	raw, err := base64.StdEncoding.DecodeString(base64Data)
// 	if err != nil {
// 		return nil, err
// 	}

// 	val, ok := r.states.Load(connectionID)
// 	var state *ConnectionState
// 	if !ok {
// 		if !bytes.HasPrefix(raw, HTTP2Preface) {
// 			return nil, nil
// 		}
// 		state = &ConnectionState{
// 			Buffer: raw[len(HTTP2Preface):],
// 		}
// 		r.states.Store(connectionID, state)
// 	} else {
// 		state = val.(*ConnectionState)
// 		state.Buffer = append(state.Buffer, raw...)
// 	}

// 	frames := []DecodedFrame{}
// 	offset := 0
// 	for {
// 		buf := state.Buffer
// 		if state.Expecting != nil {
// 			total := 9 + state.Expecting.Length
// 			if len(buf)-offset < total {
// 				break
// 			}
// 			payload := buf[offset+9 : offset+total]
// 			frames = append(frames, DecodedFrame{
// 				ConnectionID:   connectionID,
// 				Header:         state.Expecting,
// 				Payload:        payload,
// 				AdditionalInfo: decodeFrameInfo(state.Expecting, payload),
// 			})
// 			offset += total
// 			state.Expecting = nil
// 			continue
// 		}

// 		if len(buf)-offset < 9 {
// 			break
// 		}
// 		header := buf[offset : offset+9]
// 		length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
// 		typeCode := header[3]
// 		flags := header[4]
// 		streamID := binary.BigEndian.Uint32(header[5:9]) & 0x7FFFFFFF

// 		typeName, ok := HTTP2FrameTypes[typeCode]
// 		if !ok {
// 			r.states.Delete(connectionID)
// 			return frames, nil
// 		}

// 		if (typeName == "DATA" || typeName == "HEADERS") && streamID == 0 {
// 			r.states.Delete(connectionID)
// 			return frames, nil
// 		}
// 		if typeName == "SETTINGS" && length%6 != 0 {
// 			r.states.Delete(connectionID)
// 			return frames, nil
// 		}

// 		if len(buf)-offset-9 < length {
// 			if len(buf)-offset-9 == 0 {
// 				frames = append(frames, DecodedFrame{
// 					ConnectionID: connectionID,
// 					Header: &FrameHeader{
// 						Length:   length,
// 						Type:     typeName,
// 						Flags:    flags,
// 						StreamID: streamID,
// 					},
// 					Payload:    nil,
// 					Incomplete: true,
// 				})
// 			} else {
// 				state.Expecting = &FrameHeader{
// 					Length:   length,
// 					Type:     typeName,
// 					Flags:    flags,
// 					StreamID: streamID,
// 				}
// 			}
// 			break
// 		}

// 		payload := buf[offset+9 : offset+9+length]
// 		frames = append(frames, DecodedFrame{
// 			ConnectionID: connectionID,
// 			Header: &FrameHeader{
// 				Length:   length,
// 				Type:     typeName,
// 				Flags:    flags,
// 				StreamID: streamID,
// 			},
// 			Payload:        payload,
// 			AdditionalInfo: decodeFrameInfo(&FrameHeader{Length: length, Type: typeName, Flags: flags, StreamID: streamID}, payload),
// 		})
// 		offset += 9 + length
// 	}

// 	state.Buffer = state.Buffer[offset:]
// 	return frames, nil
// }

// func decodeFrameInfo(h *FrameHeader, payload []byte) string {
// 	info := fmt.Sprintf("Frame Type: %s, Length: %d, Flags: %d, Stream ID: %d\nAdditional Info:\n",
// 		h.Type, h.Length, h.Flags, h.StreamID)

// 	switch h.Type {
// 	case "SETTINGS":
// 		if len(payload)%6 != 0 {
// 			return info + "  Invalid SETTINGS frame length"
// 		}
// 		for i := 0; i+6 <= len(payload); i += 6 {
// 			id := binary.BigEndian.Uint16(payload[i : i+2])
// 			val := binary.BigEndian.Uint32(payload[i+2 : i+6])
// 			info += fmt.Sprintf("  %s = %d\n", settingName(id), val)
// 		}
// 	case "WINDOW_UPDATE":
// 		if len(payload) != 4 {
// 			info += fmt.Sprintf("  Invalid WINDOW_UPDATE frame: expected 4 bytes, got %d\n", len(payload))
// 		} else {
// 			val := binary.BigEndian.Uint32(payload)
// 			info += fmt.Sprintf("  Window Size Increment: %d\n", val&0x7FFFFFFF)
// 		}
// 	case "DATA":
// 		info += fmt.Sprintf("  Data Length: %d\n", len(payload))
// 	default:
// 		info += "  No additional info available."
// 	}

// 	return info
// }

// func settingName(id uint16) string {
// 	switch id {
// 	case 0x1:
// 		return "SETTINGS_HEADER_TABLE_SIZE"
// 	case 0x2:
// 		return "SETTINGS_ENABLE_PUSH"
// 	case 0x3:
// 		return "SETTINGS_MAX_CONCURRENT_STREAMS"
// 	case 0x4:
// 		return "SETTINGS_INITIAL_WINDOW_SIZE"
// 	case 0x5:
// 		return "SETTINGS_MAX_FRAME_SIZE"
// 	case 0x6:
// 		return "SETTINGS_MAX_HEADER_LIST_SIZE"
// 	default:
// 		return fmt.Sprintf("UNKNOWN(%d)", id)
// 	}
// }
package main

import (
	"bytes"
//	"compress/gzip"
	// "encoding/base64"
	"encoding/binary"
	"fmt"
	"golang.org/x/net/http2/hpack"
	"sync"

	pb "hub/trafficmon"
)

var (
	HTTP2Preface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	HTTP2FrameTypes = map[uint8]string{
		0x00: "DATA",
		0x01: "HEADERS",
		0x02: "PRIORITY",
		0x03: "RST_STREAM",
		0x04: "SETTINGS",
		0x05: "PUSH_PROMISE",
		0x06: "PING",
		0x07: "GOAWAY",
		0x08: "WINDOW_UPDATE",
		0x09: "CONTINUATION",
	}
)

// type FrameHeader struct {
// 	Length   int
// 	Type     string
// 	Flags    uint8
// 	StreamID uint32
// }

type ConnectionState struct {
	Buffer    []byte
	Expecting *pb.FrameHeader
	Decompressor *hpack.Decoder
}

type Reassembler struct {
	states sync.Map // map[string]*ConnectionState
}

func NewReassembler() *Reassembler {
	return &Reassembler{}
}

// type DecodedFrame struct {
// 	ConnectionID   string
// 	Header         *FrameHeader
// 	Payload        []byte
// 	Incomplete     bool
// 	AdditionalInfo string
// }

func (r *Reassembler) Feed(connectionID string, raw []byte) ([]pb.DecodedFrame, error) {
	// raw, err := base64.StdEncoding.DecodeString(base64Data)
	// if err != nil {
	// 	return nil, err
	// }

	val, ok := r.states.Load(connectionID)
	var state *ConnectionState
	if !ok {
		if !bytes.HasPrefix(raw, HTTP2Preface) {
			return nil, nil
		}
		decoder := hpack.NewDecoder(4096, nil)
		state = &ConnectionState{
			Buffer:       raw[len(HTTP2Preface):],
			Decompressor: decoder,
		}
		r.states.Store(connectionID, state)
	} else {
		state = val.(*ConnectionState)
		state.Buffer = append(state.Buffer, raw...)
	}

	frames := []pb.DecodedFrame{}
	offset := 0
	for {
		buf := state.Buffer
		if state.Expecting != nil {
			total := 9 + state.Expecting.Length
			if len(buf)-offset < int(total) {
				break
			}
			payload := buf[offset+9 : offset+int(total)]
			frames = append(frames, pb.DecodedFrame{
				ConnectionId:   connectionID,
				Header:         state.Expecting,
				Payload:        payload,
				AdditionalInfo: r.decodeFrameInfo(connectionID, state.Expecting, payload, state.Decompressor),
			})
			offset += int(total)
			state.Expecting = nil
			continue
		}

		if len(buf)-offset < 9 {
			break
		}
		header := buf[offset : offset+9]
		length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
		typeCode := header[3]
		flags := header[4]
		streamID := binary.BigEndian.Uint32(header[5:9]) & 0x7FFFFFFF

		typeName, ok := HTTP2FrameTypes[typeCode]
		if !ok {
			r.states.Delete(connectionID)
			return frames, nil
		}

		if (typeName == "DATA" || typeName == "HEADERS") && streamID == 0 {
			r.states.Delete(connectionID)
			return frames, nil
		}
		if typeName == "SETTINGS" && length%6 != 0 {
			r.states.Delete(connectionID)
			return frames, nil
		}

		if len(buf)-offset-9 < length {
			if len(buf)-offset-9 == 0 {
				frames = append(frames, pb.DecodedFrame{
					ConnectionId: connectionID,
					Header: &pb.FrameHeader{
						Length:   int32(length),
						Type:     typeName,
						Flags:    uint32(flags),
						StreamId: streamID,
					},
					Payload:    nil,
					Incomplete: true,
				})
			} else {
				state.Expecting = &pb.FrameHeader{
					Length:   int32(length),
					Type:     typeName,
					Flags:    uint32(flags),
					StreamId: streamID,
				}
			}
			break
		}

		payload := buf[offset+9 : offset+9+length]
		frames = append(frames, pb.DecodedFrame{
			ConnectionId: connectionID,
			Header: &pb.FrameHeader{
					Length:   int32(length),
					Type:     typeName,
					Flags:    uint32(flags),
					StreamId: streamID,
			},
			Payload:        payload,
			AdditionalInfo: r.decodeFrameInfo(connectionID, &pb.FrameHeader{Length: int32(length), Type: typeName, Flags: uint32(flags), StreamId: streamID}, payload, state.Decompressor),
		})
		offset += 9 + length
	}

	state.Buffer = state.Buffer[offset:]
	return frames, nil
}

func (r *Reassembler) decodeFrameInfo(connID string, h *pb.FrameHeader, payload []byte, dec *hpack.Decoder) string {
	info := fmt.Sprintf("Frame Type: %s, Length: %d, Flags: %d, Stream ID: %d\nAdditional Info:\n",
		h.Type, h.Length, h.Flags, h.StreamId)

	switch h.Type {
	case "SETTINGS":
		if len(payload)%6 != 0 {
			return info + "  Invalid SETTINGS frame length"
		}
		for i := 0; i+6 <= len(payload); i += 6 {
			id := binary.BigEndian.Uint16(payload[i : i+2])
			val := binary.BigEndian.Uint32(payload[i+2 : i+6])
			info += fmt.Sprintf("  %s = %d\n", settingName(id), val)
		}
	case "WINDOW_UPDATE":
		if len(payload) != 4 {
			info += fmt.Sprintf("  Invalid WINDOW_UPDATE frame: expected 4 bytes, got %d\n", len(payload))
		} else {
			val := binary.BigEndian.Uint32(payload)
			info += fmt.Sprintf("  Window Size Increment: %d\n", val&0x7FFFFFFF)
		}
	case "DATA":
		info += fmt.Sprintf("  Data Length: %d\n", len(payload))
	case "HEADERS":
		headers := make([]hpack.HeaderField, 0)
		dec.SetEmitFunc(func(f hpack.HeaderField) {
			headers = append(headers, f)
		})
		_, err := dec.Write(payload)
		if err != nil {
			info += fmt.Sprintf("  Failed to decode HEADERS: %v\n", err)
			break
		}
		if len(headers) == 0 {
			info += "  No headers found in HEADERS frame"
		} else {
			info += "  HEADERS:\n"
			for _, h := range headers {
				info += fmt.Sprintf("    %s: %s\n", h.Name, h.Value)
			}
		}
	default:
		info += "  No additional info available."
	}
	return info
}

func settingName(id uint16) string {
	switch id {
	case 0x1:
		return "SETTINGS_HEADER_TABLE_SIZE"
	case 0x2:
		return "SETTINGS_ENABLE_PUSH"
	case 0x3:
		return "SETTINGS_MAX_CONCURRENT_STREAMS"
	case 0x4:
		return "SETTINGS_INITIAL_WINDOW_SIZE"
	case 0x5:
		return "SETTINGS_MAX_FRAME_SIZE"
	case 0x6:
		return "SETTINGS_MAX_HEADER_LIST_SIZE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", id)
	}
}
