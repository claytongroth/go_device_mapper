package devices

import "encoding/binary"

// RTP (RFC 3550) constants. Cameras and mics are audio/video sources, so
// their UDP payload is a real RTP header instead of an arbitrary counter —
// any RTP-aware decoder should be able to parse sequence/timestamp/SSRC out
// of these bytes correctly, the same as it would from a real IP camera.
const (
	rtpVersion2 = 0x80 // version=2, padding=0, extension=0, CSRC count=0

	// Payload type numbers (RFC 3551). PT 0 (PCMU) is a real static
	// assignment for basic 8kHz audio codecs — used here for mic. Video has
	// no static PT; 96 is the conventional dynamic assignment most
	// real-world H.264 RTP streams negotiate — used here for camera.
	rtpPayloadTypePCMU = 0
	rtpPayloadTypeH264 = 96

	// RTP timestamps advance by the codec's clock rate every second, not by
	// packet count. 8kHz is standard for narrowband audio (PCMU); 90kHz is
	// the near-universal clock rate for video RTP regardless of codec.
	rtpClockRateAudio = 8000
	rtpClockRateVideo = 90000

	rtpHeaderLen = 12
)

// buildRTPPacket writes a real 12-byte RTP header into buf's first 12
// bytes (RFC 3550 section 5.1) — version/payload-type/sequence/timestamp/
// SSRC — leaving the rest of buf as filler standing in for encoded media.
func buildRTPPacket(buf []byte, payloadType byte, seq uint16, timestamp, ssrc uint32) {
	buf[0] = rtpVersion2
	buf[1] = payloadType // marker bit left unset (top bit 0)
	binary.BigEndian.PutUint16(buf[2:4], seq)
	binary.BigEndian.PutUint32(buf[4:8], timestamp)
	binary.BigEndian.PutUint32(buf[8:12], ssrc)
}

// CoAP (RFC 7252) constants — the real IETF protocol constrained IoT
// devices use to push telemetry over UDP, which is exactly the role
// iot-* devices play here (see cloudAddr in main.go).
const (
	coapVer1NonConfirmable = 0x50 // Ver=1 (01), Type=NON (01), TKL=0 (0000)
	coapCodePOST           = 0x02 // Code 0.02 = POST
	coapPayloadMarker      = 0xFF
	coapHeaderLen          = 4
)

// buildCoAPPacket writes a real 4-byte CoAP header into buf (RFC 7252
// section 3) plus the 0xFF payload marker, leaving the rest of buf as
// filler standing in for the actual sensor reading.
func buildCoAPPacket(buf []byte, messageID uint16) {
	buf[0] = coapVer1NonConfirmable
	buf[1] = coapCodePOST
	binary.BigEndian.PutUint16(buf[2:4], messageID)
	buf[coapHeaderLen] = coapPayloadMarker
}
