package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"hash/fnv"
	"io"
	"sync"
)

// GzipDecompressor tracks gzip streams by interleaved stream ID
type GzipDecompressor struct {
	streams sync.Map // map[string][][]byte
}

func NewGzipDecompressor() *GzipDecompressor {
	return &GzipDecompressor{}
}

func (g *GzipDecompressor) getInterleavedID(connectionID string, streamID uint32) string {
	idBytes, err := hex.DecodeString(connectionID[len(connectionID)-8:])
	if err != nil || len(idBytes) != 4 {
		// fallback: use hash
		h := fnv.New32a()
		h.Write([]byte(connectionID))
		idBytes = make([]byte, 4)
		binary.BigEndian.PutUint32(idBytes, h.Sum32())
	}
	sidBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(sidBytes, streamID)

	interleaved := make([]byte, 4)
	for i := 0; i < 4; i++ {
		interleaved[i] = idBytes[i] ^ sidBytes[i]
	}
	return hex.EncodeToString(interleaved)
}

func (g *GzipDecompressor) concatBuffers(buffers [][]byte) []byte {
	total := 0
	for _, b := range buffers {
		total += len(b)
	}
	joined := make([]byte, total)
	offset := 0
	for _, b := range buffers {
		copy(joined[offset:], b)
		offset += len(b)
	}
	return joined
}

func (g *GzipDecompressor) gunzipData(data []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer r.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, r)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Feed accumulates gzip data per stream and returns decompressed text if end-of-stream.
func (g *GzipDecompressor) Feed(connectionID string, streamID uint32, payload []byte, flags uint8) (string, error) {
	if len(payload) < 2 {
		return "", nil
	}
	isGzip := payload[0] == 0x1f && payload[1] == 0x8b
	id := g.getInterleavedID(connectionID, streamID)

	val, ok := g.streams.Load(id)
	if isGzip || ok {
		if !ok {
			g.streams.Store(id, [][]byte{payload})
		} else {
			g.streams.Store(id, append(val.([][]byte), payload))
		}

		if flags&0x1 == 0x1 { // END_STREAM
			chunks, _ := g.streams.Load(id)
			g.streams.Delete(id)
			joined := g.concatBuffers(chunks.([][]byte))
			text, err := g.gunzipData(joined)
			if err != nil {
				return "", err
			}
			return text, nil
		}
	}
	return "", nil
}
