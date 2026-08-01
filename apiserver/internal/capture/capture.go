package capture

import (
	"apiserver/internal/decode"
	"apiserver/internal/graph"
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"golang.org/x/sync/errgroup"
)

const LogAllPackets = false

const (
	bufferSize  = 10
	workerCount = 3
)

// make a struct for

// Take in the packet data and decide various things about it.
func classifyPacket(packet gopacket.Packet) decode.PacketData {
	var packetKind string
	if packet.Layer(layers.LayerTypeTCP) != nil {
		packetKind = "TCP"
	} else if packet.Layer(layers.LayerTypeUDP) != nil {
		packetKind = "UDP"
	} else {
		packetKind = "unknown"
	}

	// The BPF filter is "tcp or udp", which matches both IPv4 and IPv6
	// traffic — so a bare packet.Layer(layers.LayerTypeIPv4) assertion isn't
	// safe here. The single-value type-assertion form (ipLayer.(*layers.IPv4))
	// panics if ipLayer turned out to be nil (no IPv4 layer at all, e.g. an
	// IPv6 packet), so we check for IPv4 first, fall back to IPv6, and use
	// the two-value ", ok" form so a type mismatch degrades to zero-value
	// IPs instead of crashing the whole capture loop.
	var srcIP, dstIP net.IP
	if ipv4Layer := packet.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
		if ip, ok := ipv4Layer.(*layers.IPv4); ok {
			srcIP, dstIP = ip.SrcIP, ip.DstIP
		}
	} else if ipv6Layer := packet.Layer(layers.LayerTypeIPv6); ipv6Layer != nil {
		if ip, ok := ipv6Layer.(*layers.IPv6); ok {
			srcIP, dstIP = ip.SrcIP, ip.DstIP
		}
	}

	// packet.Data() is the *entire* captured frame — loopback header, then
	// IP header, then TCP/UDP header, then finally the actual payload. RTP/
	// CoAP/HTTP bytes only start at the payload, not at byte 0 of the whole
	// frame. packet.ApplicationLayer() gives us exactly that: whatever's
	// left after every recognized header has been peeled off. It's nil if
	// there's no payload at all (e.g. a bare ACK with no data).
	var appPayload []byte
	if app := packet.ApplicationLayer(); app != nil {
		appPayload = app.Payload()
	}

	// 32 bytes was enough for RTP (12-byte header) and CoAP (4-byte header), on the cheap for now
	rawSliceLen := 512
	packetRawData := appPayload[:min(len(appPayload), rawSliceLen)]

	return decode.PacketData{
		PacketKind:   packetKind,
		SourceIP:     srcIP,
		DestIP:       dstIP,
		PacketLength: len(packet.Data()),
		RawData:      packetRawData,
	}
}

// Stringify the packet as a PacketData struct
func stringifyPacket(packet decode.PacketData) string {
	return fmt.Sprintf("Kind: %s, Traffic: %s -> %s len=%d, raw=%x", packet.PacketKind, packet.SourceIP, packet.DestIP, packet.PacketLength, packet.RawData)
}

// Producer, only ever SENDS TO the channel
func chewPackets(ctx context.Context, out chan<- decode.PacketData, handle *pcap.Handle) error {
	defer close(out)

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	for packet := range packetSource.Packets() {
		// get the details of the packet and hold in a struct
		detailedPacket := classifyPacket(packet)

		// stringify
		packetAsString := stringifyPacket(detailedPacket)
		if LogAllPackets == true {
			slog.Info("Here is the packet as string: ", "info", packetAsString)
		}

		// Send detailed packet on the channel so that the workers can get it
		out <- detailedPacket

	}

	// no need to return
	return nil
}

// Consumer, only ever READS from the channel
func packetWorker(id int, in <-chan decode.PacketData, store *graph.Store) error {

	// Implicitly read from the channel here
	for packetToProcess := range in {
		// decode.DecodePacketData returns a decode.DecodedPacketData,
		// which satisfies fmt.Stringer purely because DecodedPayload
		// is embedded in it (see decode.go) — so it can be logged
		// directly with %s/%v, same as any other Stringer.
		decodedPacketData := decode.DecodePacketData(packetToProcess)
		if LogAllPackets == true {
			slog.Info("Decoded packet", "info", decodedPacketData.String())
		}

		// Store the data in graphstore
		store.AddEdge(packetToProcess.SourceIP, packetToProcess.DestIP, packetToProcess)
		if LogAllPackets == true {
			slog.Info("Worker processed packet", "id", id, "info", stringifyPacket(packetToProcess))
		}
	}
	return nil
}

// Take a pointer to *graph.Store
// Uppercase so it exports.
func OngoingCapture(ctx context.Context, store *graph.Store) error {

	// Declare a channel for all our worker go routines that will be consuming packets
	captureChan := make(chan decode.PacketData, bufferSize)

	// Declare and errorgroup to handle shutdown
	g, ctx := errgroup.WithContext(ctx)

	// Assign handle to pcap
	if handle, err := pcap.OpenLive("lo", 1600, true, pcap.BlockForever); err != nil {
		// if we get and error getting pcap
		slog.Error("Failed to listen")
		panic(err)
	} else if err := handle.SetBPFFilter("tcp or udp"); err != nil { // capture all UDP and TCP traffic
		slog.Error("Failed to SetBPFFilter")
		panic(err)
	} else {
		slog.Info("Listening for packets...")
		// declare the source from packets handle

		// call the chewPackets function in a go-routine with this context
		// Add this function to the error group
		g.Go(func() error {
			// In the background, chew through the packets and process them.
			// Once they are processed, put them on the channel for the workers to decode and add to the graph
			return chewPackets(ctx, captureChan, handle)
		})

		// loop for the count of workers we have
		for i := 0; i < workerCount; i++ {
			id := i
			// Add this function to the error group
			g.Go(func() error {
				// run workers to decode the packets and put them on the graph
				return packetWorker(id, captureChan, store)
			})
		}
	}

	// wait for the ctx error group
	return g.Wait()

}
