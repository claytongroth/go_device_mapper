package devices

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net"
	"time"
)

// basicPusher sends UDP datagrams to Target at a fixed interval. It writes
// a real per-class header into each payload — RTP (RFC 3550) for
// camera/mic, CoAP (RFC 7252) for iot (see protocols.go) — so a decoder on
// the other end has genuine wire-format bytes to parse instead of an
// arbitrary counter. The one thing still missing versus chunk 6's eventual
// real devices is the surrounding behavior (actual video/audio encoding,
// real sensor readings, jitter/loss derivation) — the header bytes
// themselves are already the real thing. Workstations moved off this onto
// httpWorkstation (httpworkstation.go), which does real HTTP/TCP.
type basicPusher struct {
	cfg Config
}

func init() {
	f := func(cfg Config) Device { return &basicPusher{cfg: cfg} }
	Register(TypeCamera, f)
	Register(TypeMic, f)
	Register(TypeIoT, f)
	// TypeRogue is intentionally unregistered: its port-sweep behavior is
	// distinct enough that it gets its own implementation in chunk 6e.
}

// Declare a Info() method for Info that returns an Info instance, basically a getter
func (p *basicPusher) Info() Info {
	return Info{
		ID:     p.cfg.ID,
		Type:   p.cfg.Type,
		IP:     p.cfg.BindIP,
		Vendor: p.cfg.Vendor,
		Name:   p.cfg.Name,
	}
}

// Run is the method that makes *basicPusher satisfy the Device interface's
// Run(ctx) requirement (see device.go). `(p *basicPusher)` before the name
// is the method receiver — it's what makes this a method on basicPusher
// instead of a standalone function, and lets the body refer to p.cfg. Yes:
// calling this is literally "run the device" — it opens a socket and loops
// sending packets until ctx says stop.
func (p *basicPusher) Run(ctx context.Context) error {

	// Parse the Target string ("ip:port") into a structured *net.UDPAddr.
	// "raddr" = remote address — the peer/cloud endpoint this device sends
	// its packets to.
	raddr, err := net.ResolveUDPAddr("udp4", p.cfg.Target)
	if err != nil {
		return fmt.Errorf("basicPusher %s: resolve target %q: %w", p.cfg.ID, p.cfg.Target, err)
	}

	// "laddr" = local address: the source side of the socket. Giving it
	// only an IP (no Port) tells the OS "bind to this specific IP, but pick
	// any free port for me." This is the important bit for the whole
	// simulation: it's what makes each device's packets actually originate
	// from its own 127.x.y.z address instead of the default 127.0.0.1.
	laddr := &net.UDPAddr{IP: p.cfg.BindIP}

	// DialUDP opens the actual socket. UDP has no real "connection" on the
	// wire, but DialUDP still gives us a "connected" socket locally: it
	// remembers raddr for us so later conn.Write() calls don't need to
	// repeat the destination each time, and it locks the source to laddr.
	conn, err := net.DialUDP("udp4", laddr, raddr)
	if err != nil {
		return fmt.Errorf("basicPusher %s: dial from %s: %w", p.cfg.ID, p.cfg.BindIP, err)
	}
	defer conn.Close()

	// Each class needs at least enough bytes for its real header (RTP's 12,
	// CoAP's 4-plus-marker); PayloadSize from Config already comfortably
	// exceeds both for every class currently configured (see swarm.go), but
	// this keeps that an explicit guarantee rather than an assumption.
	minLen := 8
	switch p.cfg.Type {
	case TypeCamera, TypeMic:
		minLen = rtpHeaderLen
	case TypeIoT:
		minLen = coapHeaderLen + 1 // +1 for the payload marker byte
	}
	payload := make([]byte, max(p.cfg.PayloadSize, minLen))

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	// ssrc identifies this device's RTP stream and stays fixed for the
	// whole run, the same way a real camera/mic uses one SSRC per session.
	ssrc := rand.Uint32()
	payloadType := byte(rtpPayloadTypeH264)
	clockRate := uint32(rtpClockRateVideo)
	if p.cfg.Type == TypeMic {
		payloadType = rtpPayloadTypePCMU
		clockRate = rtpClockRateAudio
	}

	var seq uint16
	var timestamp uint32
	for {
		select {
		// ctx itself isn't a channel — context.Context is an interface with
		// a Done() method that returns one. That channel starts empty/open
		// and gets closed the moment ctx is cancelled. A `select` with two
		// `case <-channel:` arms blocks until whichever channel is ready
		// first, so this waits for either "shutdown" or "next tick",
		// whichever happens first.
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// seq doubles as this packet's identity for whichever header
			// format applies (RTP sequence number, CoAP message ID) — same
			// role the old bare counter played, but now inside a real
			// protocol's actual header fields instead of ad hoc bytes.
			switch p.cfg.Type {
			case TypeCamera, TypeMic:
				buildRTPPacket(payload, payloadType, seq, timestamp, ssrc)
				timestamp += uint32(p.cfg.Interval.Seconds() * float64(clockRate))
			case TypeIoT:
				buildCoAPPacket(payload, seq)
			default:
				binary.BigEndian.PutUint64(payload[:8], uint64(seq))
			}

			if _, err := conn.Write(payload); err != nil {
				return fmt.Errorf("basicPusher %s: write: %w", p.cfg.ID, err)
			}
			seq++
		}
	}
}
