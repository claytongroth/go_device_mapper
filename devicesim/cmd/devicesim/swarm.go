package main

import (
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"devicesim/internal/devices"
)

// classProfile bundles the per-class constants (target, interval, payload,
// vendor) that stay fixed no matter how many devices of that class get
// randomly generated — only the count, per-device identity, and IP vary
// between runs. workstation's target is left blank here since it doesn't
// get one until wireWorkstationRing assigns it a real peer below.
//
// weight controls how often this class gets picked relative to the others
// (see pickClass) — not a probability itself, just a share of the total.
type classProfile struct {
	deviceType  devices.Type
	weight      int
	target      string
	interval    time.Duration
	payloadSize int
	vendor      string
}

// Weights are chosen to look like a real office network rather than an
// even split across classes: workstations (end-user machines) vastly
// outnumber specialized infrastructure like cameras/mics/IoT sensors.
var classProfiles = []classProfile{
	{devices.TypeWorkstation, 70, "", 1 * time.Second, 0, "Dell"},
	{devices.TypeIoT, 15, cloudAddr, 1 * time.Second, 128, "Acme"},
	{devices.TypeCamera, 10, avServerAddr, 1 * time.Second, 200, "Hikview"},
	{devices.TypeMic, 5, avServerAddr, 1 * time.Second, 64, "Shure"},
	// TypeRogue is intentionally excluded — no factory is registered for it
	// yet (see pusher.go), so devices.New would just fail for it today.
}

// pickClass picks a random classProfile weighted by its weight field,
// instead of picking uniformly among classProfiles — e.g. with the
// weights above, a workstation is 14x as likely to come up as a mic.
func pickClass() classProfile {
	total := 0
	for _, p := range classProfiles {
		total += p.weight
	}

	r := rand.IntN(total)
	for _, p := range classProfiles {
		if r < p.weight {
			return p
		}
		r -= p.weight
	}
	panic("unreachable: r should always fall within one class's weight range")
}

// randomSwarm builds a swarm of random size (1-500 devices) each time it's
// called, picking a random class per device from classProfiles. Only the
// count, per-device identity (ID/name/IP), and workstation peering are
// randomized — each class's traffic profile (target/interval/payload)
// stays the same "sensible" values every camera/mic/iot already used.
func randomSwarm() []devices.Config {
	size := 1 + rand.IntN(500)
	swarm := make([]devices.Config, size)
	counts := make(map[devices.Type]int)

	for i := range swarm {
		p := pickClass()
		counts[p.deviceType]++
		n := counts[p.deviceType]

		swarm[i] = devices.Config{
			ID:          fmt.Sprintf("%s-%d", p.deviceType, n),
			Type:        p.deviceType,
			BindIP:      nextIP(i),
			Target:      p.target,
			Interval:    p.interval,
			PayloadSize: p.payloadSize,
			Vendor:      p.vendor,
			Name:        fmt.Sprintf("%s-%03d", p.deviceType, n),
		}
	}

	wireWorkstationRing(swarm)
	return swarm
}

// nextIP hands out a unique 127.1.x.y address per index, spreading i
// across the third and fourth octets (room for 65536 devices — comfortably
// more than the 500-device cap). 127.1.x.y is a separate range from the
// fixed 127.0.x.y addresses (avServerAddr, cloudAddr) used elsewhere, so a
// randomized swarm can never collide with them.
func nextIP(i int) net.IP {
	return net.IPv4(127, 1, byte(i>>8), byte(i))
}

// wireWorkstationRing points each workstation at the next workstation in
// swarm, wrapping the last back to the first — the same ring shape
// main.go used to wire by hand for a fixed 4, just generalized to however
// many workstations this particular random draw produced. With modulo
// wraparound, a lone workstation degrades gracefully to targeting itself
// rather than needing a special case.
func wireWorkstationRing(swarm []devices.Config) {
	var idx []int
	for i, cfg := range swarm {
		if cfg.Type == devices.TypeWorkstation {
			idx = append(idx, i)
		}
	}
	for pos, i := range idx {
		next := idx[(pos+1)%len(idx)]
		swarm[i].Target = fmt.Sprintf("%s:%s", swarm[next].BindIP, workstationHTTPPort)
	}
}
