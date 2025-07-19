package main

import (
	// "encoding/base64"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	pb "hub/trafficmon"
)


const (
	AF_UNSPEC       = 0
	AF_INET         = 2
	AF_INET6        = 10
	EVENT_TYPE_WRITE = 1 // Replace with actual constant
)

func reverseEndian(n uint32) uint32 {
	if n <= 0xFFFF {
		// 16-bit
		return ((n & 0xFF) << 8) | ((n >> 8) & 0xFF)
	} else if n <= 0xFFFFFFFF {
		// 32-bit
		return ((n & 0xFF) << 24) |
			((n & 0xFF00) << 8) |
			((n >> 8) & 0xFF00) |
			((n >> 24) & 0xFF)
	}
	panic("Only 16-bit and 32-bit integers are supported.")
}

func decodeBase64IP(bytes []byte) (string, error) {
	if len(bytes) != 16 {
		return "", errors.New("Invalid IPv6 address length")
	}

	// Check if it's IPv4-mapped IPv6
	if isIPv4MappedIPv6(bytes) {
		return fmt.Sprintf("%d.%d.%d.%d", bytes[12], bytes[13], bytes[14], bytes[15]), nil
	}

	// Convert to standard IPv6 string
	var parts []string
	for i := 0; i < 16; i += 2 {
		part := binary.BigEndian.Uint16(bytes[i : i+2])
		parts = append(parts, fmt.Sprintf("%x", part))
	}
	return compressIPv6(parts), nil
}

func isIPv4MappedIPv6(b []byte) bool {
	for i := 0; i < 10; i++ {
		if b[i] != 0 {
			return false
		}
	}
	return b[10] == 0xff && b[11] == 0xff
}

func compressIPv6(parts []string) string {
	result := strings.Join(parts, ":")
	result = strings.Replace(result, ":0:0:0:0:0:0:0:", "::", 1)
	result = strings.Replace(result, ":0:0:0:0:0:0:", "::", 1)
	result = strings.Replace(result, ":0:0:0:0:0:", "::", 1)
	return result
}

// uint32ToIPv4 converts a uint32 IP (in network byte order) to string format (e.g., "192.168.1.1")
func uint32ToIPv4(ip uint32) string {
	bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(bytes, ip)
	return net.IP(bytes).String()
}

func getNormalizedAddressInfo(addr *pb.UprobeAddressInfo, eventType uint32) (*pb.NormalizedAddrInfo) {
	normalizedAddrInfo := &pb.NormalizedAddrInfo{
		SrcIP:   "-",
		DstIP:   "-",
		SrcPort: 0,
		DstPort: 0,
	}
	if addr == nil {
		return normalizedAddrInfo
	}

	isWrite := eventType == EVENT_TYPE_WRITE

	if addr.Family == AF_INET {
		if isWrite {
			normalizedAddrInfo.SrcIP = uint32ToIPv4(addr.Saddr4)
			normalizedAddrInfo.DstIP = uint32ToIPv4(addr.Daddr4)
			normalizedAddrInfo.SrcPort = int32(reverseEndian(addr.Sport))
			normalizedAddrInfo.DstPort = int32(reverseEndian(addr.Dport))
		} else {
			normalizedAddrInfo.SrcIP = uint32ToIPv4(addr.Daddr4)
			normalizedAddrInfo.DstIP = uint32ToIPv4(addr.Saddr4)
			normalizedAddrInfo.SrcPort = int32(reverseEndian(addr.Dport))
			normalizedAddrInfo.DstPort = int32(reverseEndian(addr.Sport))
		}
	} else if addr.Family == AF_INET6 {
		var err error
		if isWrite {
			normalizedAddrInfo.SrcIP, err = decodeBase64IP(addr.Saddr6)
			normalizedAddrInfo.DstIP, err = decodeBase64IP(addr.Daddr6)
			normalizedAddrInfo.SrcPort = int32(reverseEndian(addr.Sport))
			normalizedAddrInfo.DstPort = int32(reverseEndian(addr.Dport))
		} else {
			normalizedAddrInfo.SrcIP, err = decodeBase64IP(addr.Daddr6)
			normalizedAddrInfo.DstIP, err = decodeBase64IP(addr.Saddr6)
			normalizedAddrInfo.SrcPort = int32(reverseEndian(addr.Dport))
			normalizedAddrInfo.DstPort = int32(reverseEndian(addr.Sport))
		}
		if err != nil {
			normalizedAddrInfo.SrcIP = "?"
			normalizedAddrInfo.DstIP = "?"
		}
	} else {
		normalizedAddrInfo.SrcIP, normalizedAddrInfo.DstIP = "-", "-"
		normalizedAddrInfo.SrcPort, normalizedAddrInfo.DstPort = 0, 0
	}
	return normalizedAddrInfo
}

func generateEventID(ts uint64, tid uint64, data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%016x%016x", ts, tid)
	n := min(len(data), 16)
	h.Write(data[:n])
	return hex.EncodeToString(h.Sum(nil))[:16]
}


func GetConnectionID(srcIP string, dstIP string, srcPort int, dstPort int) string {
	endpoint1 := fmt.Sprintf("%s:%d", srcIP, srcPort)
	endpoint2 := fmt.Sprintf("%s:%d", dstIP, dstPort)

	endpoints := []string{endpoint1, endpoint2}
	sort.Strings(endpoints) // lexicographical sort

	connectionString := fmt.Sprintf("%s--%s", endpoints[0], endpoints[1])

	hash := sha1.Sum([]byte(connectionString)) // returns [20]byte
	return hex.EncodeToString(hash[:])
}