package decode

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
)

type PacketData struct {
	PacketKind   string
	SourceIP     net.IP
	DestIP       net.IP
	PacketLength int
	RawData      []byte
}

// DecodedPayload is whatever a protocol-specific decoder pulled out of a
// packet's raw bytes. RTPData/CoAPData/UnknownData below are three
// different concrete types that all satisfy this interface — same "one
// interface, many concrete implementations" shape as devicesim's
// devices.Device (basicPusher/httpWorkstation).
//
// Embedding fmt.Stringer here (rather than writing out `String() string`
// by hand) means: "anything that satisfies DecodedPayload must also
// satisfy fmt.Stringer." Any type with a String() string method already
// satisfies fmt.Stringer, so this is just reusing a standard-library
// interface instead of redeclaring an identical one.
type DecodedPayload interface {
	fmt.Stringer
}

// Wire-format marker bytes that identify which protocol produced a
// packet — must match devicesim/internal/devices/protocols.go exactly,
// since that's what's actually writing these bytes on the wire.
const (
	rtpVersionByte  = 0x80 // RTP version 2, no padding/extension/CSRC
	rtpHeaderLen    = 12
	coapVersionByte = 0x50 // CoAP Ver=1, Type=NON, TKL=0
	coapHeaderLen   = 4
)

// RTPData holds the fields parsed out of a real RTP header (RFC 3550).
type RTPData struct {
	PayloadType byte
	Sequence    uint16
	Timestamp   uint32
	SSRC        uint32
}

// String is what makes RTPData satisfy fmt.Stringer, and therefore
// DecodedPayload — no separate "implements" declaration needed, Go checks
// this structurally, just by RTPData having a method with this exact name
// and signature.
func (r RTPData) String() string {
	return fmt.Sprintf("RTP pt=%d seq=%d ts=%d ssrc=%08x", r.PayloadType, r.Sequence, r.Timestamp, r.SSRC)
}

// CoAPData holds the fields parsed out of a real CoAP header (RFC 7252).
type CoAPData struct {
	Code      byte
	MessageID uint16
}

func (c CoAPData) String() string {
	return fmt.Sprintf("CoAP code=%#x msgID=%d", c.Code, c.MessageID)
}

// UnknownData is the fallback when RawData doesn't match any protocol
// this package knows how to parse yet.
type UnknownData struct{}

func (UnknownData) String() string {
	return "undecoded payload"
}

// HTTPData holds the fields parsed out of a real HTTP request — the
// genuine wire format devicesim's httpWorkstation ring speaks over TCP.
type HTTPData struct {
	Method string
	Path   string
	Host   string
}

func (h HTTPData) String() string {
	return fmt.Sprintf("HTTP %s %s host=%s", h.Method, h.Path, h.Host)
}

// httpMethodPrefixes are the request-line tokens that mark a payload as
// HTTP. Unlike RTP/CoAP, HTTP has no single magic byte at a fixed offset —
// it's ASCII text, so detection means checking whether the payload starts
// with one of these method tokens instead of comparing one byte.
var httpMethodPrefixes = [][]byte{
	[]byte("GET "), []byte("POST "), []byte("PUT "),
	[]byte("DELETE "), []byte("HEAD "), []byte("OPTIONS "), []byte("PATCH "),
}

func looksLikeHTTPRequest(data []byte) bool {
	for _, prefix := range httpMethodPrefixes {
		if bytes.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

// parseHTTPRequest uses the standard library's real HTTP parser —
// http.ReadRequest — instead of hand-parsing method/path/headers by byte
// offset the way RTP/CoAP do; HTTP's variable-length text format doesn't
// have fixed offsets to read from. It wants a *bufio.Reader, so data gets
// wrapped first through bytes.NewReader (turns the []byte into an
// io.Reader) and then bufio.NewReader (adds the buffering ReadRequest
// needs to peek at lines). Falls back to UnknownData if data is truncated
// or otherwise doesn't parse as a complete request.
func parseHTTPRequest(data []byte) DecodedPayload {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return UnknownData{}
	}
	return HTTPData{
		Method: req.Method,
		Path:   req.URL.Path,
		Host:   req.Host,
	}
}

// DecodedPacketData bundles the original captured facts (OriginalData)
// with whatever got decoded from them. DecodedPayload here is an embedded
// (anonymous) field, not a named one — that promotes its String() method
// up onto DecodedPacketData itself, so a DecodedPacketData value already
// satisfies fmt.Stringer too, with no extra code required.
type DecodedPacketData struct {
	OriginalData PacketData
	DecodedPayload
}

// DecodePacketData looks at packet's kind and the first few raw bytes to
// figure out which protocol produced it, then parses that protocol's real
// header fields into the matching concrete DecodedPayload type.
func DecodePacketData(packet PacketData) DecodedPacketData {
	var decoded DecodedPayload

	switch {
	// RTP for UDP
	case packet.PacketKind == "UDP" && len(packet.RawData) >= rtpHeaderLen && packet.RawData[0] == rtpVersionByte:
		decoded = RTPData{
			PayloadType: packet.RawData[1],
			Sequence:    binary.BigEndian.Uint16(packet.RawData[2:4]),
			Timestamp:   binary.BigEndian.Uint32(packet.RawData[4:8]),
			SSRC:        binary.BigEndian.Uint32(packet.RawData[8:12]),
		}
	// CoAp for HTTP
	case packet.PacketKind == "UDP" && len(packet.RawData) >= coapHeaderLen && packet.RawData[0] == coapVersionByte:
		decoded = CoAPData{
			Code:      packet.RawData[1],
			MessageID: binary.BigEndian.Uint16(packet.RawData[2:4]),
		}
	case packet.PacketKind == "TCP" && looksLikeHTTPRequest(packet.RawData):
		decoded = parseHTTPRequest(packet.RawData)
	default:
		decoded = UnknownData{}
	}

	return DecodedPacketData{
		OriginalData:   packet,
		DecodedPayload: decoded,
	}
}
