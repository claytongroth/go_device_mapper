// Command devicesim launches the simulated device swarm on loopback IPs,
// run via Docker Compose (see docker-compose.yml / scripts/dev.sh).
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"devicesim/internal/devices"
)

// In a real deployment, a camera or thermostat is configured once (by an
// installer, or baked into firmware) with the address of one well-known
// server it reports to for its whole life — it never "discovers" that
// server dynamically. avServerAddr and cloudAddr model two such servers:
// cameras/mic push to avServerAddr (standing in for an NVR/media gateway),
// iot pushes to cloudAddr (standing in for a cloud telemetry endpoint).
// Both are still just discard listeners (runSink), same as before — the
// realism improvement is having two distinct destinations instead of one
// shared by every device class.
const (
	avServerAddr = "127.0.9.1:9999" // This is where things get sent to within network... Media gateway...
	cloudAddr    = "127.0.9.2:9999"
)

// workstationHTTPPort is the port every workstation's own HTTP server (see
// httpWorkstation in internal/devices) listens on, and the port peers call
// it back on — real TCP/HTTP traffic, not a UDP stand-in.
const workstationHTTPPort = "8000"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// signal.NotifyContext returns a context.Context that starts out "live"
	// and gets cancelled automatically the instant this process receives one
	// of the listed OS signals (Ctrl-C sends os.Interrupt; `kill <pid>` sends
	// SIGTERM by default). Every goroutine below watches ctx via ctx.Done(),
	// so one Ctrl-C tells all of them to stop instead of us wiring up
	// shutdown signaling by hand for each device. stop() just unregisters
	// the signal handler; deferring it is cleanup so a second Ctrl-C after
	// we've already started shutting down behaves like a normal interrupt.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// Both servers are started the same way, so startSink (below) holds the
	// wg.Add(1)/go func()/error-logging boilerplate once instead of us
	// repeating it per address.
	startSink(ctx, &wg, avServerAddr)
	startSink(ctx, &wg, cloudAddr)

	// randomSwarm (swarm.go) picks a random size (1-500) and a random class
	// per device each run, wiring any workstations it generates into a ring
	// the same way the old fixed 4-workstation swarm was wired by hand.
	swarm := randomSwarm()

	// loop through the swarm and concurrently add a device for each item
	for _, cfg := range swarm {
		// devices.New looks cfg.Type up in the registry (registry.go) and
		// calls whichever factory function was registered for it — e.g. the
		// basicPusher factory for TypeCamera. What we get back is a
		// devices.Device interface value; this loop never needs to know the
		// concrete type underneath it.
		dev, err := devices.New(cfg)
		if err != nil {
			slog.Error("failed to construct device", "id", cfg.ID, "err", err)
			continue
		}
		wg.Add(1)
		go func(d devices.Device, cfg devices.Config) {
			defer wg.Done()
			// d.Info() is an interface method call: Go picks the concrete
			// type's implementation at runtime (here, basicPusher.Info())
			// based on what d actually holds, even though this code only
			// ever refers to the devices.Device interface.
			info := d.Info()
			logDeviceStarting(info, cfg)
			if err := d.Run(ctx); err != nil {
				logDeviceStopped(info, err)
			}
		}(dev, cfg)
	}

	slog.Info("swarm running", "count", len(swarm), "avServer", avServerAddr, "cloud", cloudAddr)
	// ctx.Done() returns a channel that gets closed the instant ctx is
	// cancelled. Receiving from a closed channel never blocks, so this line
	// sits here doing nothing until shutdown is triggered (Ctrl-C/SIGTERM),
	// then immediately falls through to the shutdown logic below.
	<-ctx.Done()
	slog.Info("shutting down")
	wg.Wait()
	slog.Info("shutdown complete")
}

// startSink launches runSink in its own goroutine and registers it with wg,
// so main can spin up multiple server-style listeners (avServerAddr,
// cloudAddr, ...) without repeating the same wg.Add(1)/go func()/error-log
// boilerplate for each one. Taking *sync.WaitGroup (a pointer) matters here:
// wg.Add/wg.Done both mutate wg's internal counter, and main needs to see
// those mutations through the *same* WaitGroup it later calls wg.Wait() on —
// passing sync.WaitGroup by value would copy it, and the copy's Add/Done
// calls would never be seen by main's Wait().
func startSink(ctx context.Context, wg *sync.WaitGroup, addr string) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := runSink(ctx, addr); err != nil {
			slog.Error("sink stopped", "addr", addr, "err", err)
		}
	}()
}

// runSink is a throwaway UDP discard listener, not a devices.Device — it
// just gives the "push to a known server" device classes (camera/mic/iot)
// somewhere real to send packets to. Workstations don't use this at all —
// they're configured to target each other directly (see main).
//
// ctx is how the caller (main) tells this function "stop now" without main
// needing to know anything about UDP sockets. runSink doesn't create ctx —
// it's just handed one, and watches it via ctx.Done() further down to know
// when to quit.
func runSink(ctx context.Context, addr string) error {
	// The devices send UDP (not TCP) because RTP/telemetry-style traffic in
	// this project is fire-and-forget, connectionless packets — no
	// handshake, no guaranteed delivery, which matches real cameras/mics/IoT
	// sensors and lets lost packets show up as a real, measurable metric
	// later. ResolveUDPAddr just parses the "ip:port" string into the
	// structured *net.UDPAddr the socket calls below need.
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return err
	}
	// ListenUDP opens an actual OS socket bound to udpAddr and hands back a
	// *net.UDPConn we can read incoming packets from — this is what lets the
	// sink receive whatever the devices send to it.
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		// conn.ReadFromUDP below blocks until a packet arrives — it has no
		// way to notice ctx being cancelled on its own. This goroutine's
		// only job is to wait for shutdown and then force-close conn, which
		// makes the blocked ReadFromUDP call return immediately with an
		// error, so the loop below can see ctx is done and exit cleanly.
		<-ctx.Done()
		conn.Close()
	}()

	// A reusable buffer to receive each incoming packet's bytes into; 2048
	// is comfortably larger than anything this swarm sends, so packets
	// won't get truncated. ReadFromUDP overwrites it on every iteration
	// rather than allocating a new slice per packet.
	buf := make([]byte, 2048)
	for {
		if _, _, err := conn.ReadFromUDP(buf); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}
