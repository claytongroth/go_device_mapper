// Package devices defines the simulated device classes that make up the
// virtual network swarm: their identity/metadata and the behavior contract
// the launcher uses to run them.
package devices

import (
	"context"
	"net"
	"time"
)

// Type is a device class. It's string-typed (not iota-int) because the API
// spec filters assets by this exact value, e.g. GET /assets?type=camera.
type Type string

const (
	TypeCamera      Type = "camera"
	TypeMic         Type = "mic"
	TypeIoT         Type = "iot"
	TypeWorkstation Type = "workstation"
	TypeRogue       Type = "rogue"
)

// Info is a device's identity/metadata — the simulation's ground truth.
// The collector never reads this directly; it infers the same facts
// passively from captured packets. Info exists so the launcher can report
// what it spun up, and so later we can check the collector's inference
// against what's actually true.
type Info struct {
	ID     string
	Type   Type
	IP     net.IP
	Vendor string
	Name   string
}

// Device is the behavior contract every simulated device implements. Run
// blocks, emitting traffic, until ctx is cancelled.
//
// An interface in Go is just a list of method signatures. Any concrete type
// that has methods matching that list automatically satisfies the
// interface — there's no "implements Device" keyword to write, unlike
// Java/C#. basicPusher (pusher.go) satisfies this today just by having
// Info() and Run(ctx) methods with these exact signatures; a future
// rtpCamera struct with real jitter/loss logic would satisfy it the same
// way, with zero changes needed here.
//
// The payoff: code like registry.go's Factory map, or main.go's launch
// loop, can hold a Device value and call .Info()/.Run() on it without ever
// knowing or caring which concrete struct is actually underneath. Go picks
// the right method at runtime based on what's actually stored. That's what
// lets chunk 6 add five more device classes later without touching main.go
// or the registry at all.
type Device interface {
	Info() Info
	Run(ctx context.Context) error
}

// Config is the construction-time configuration for a device. Concrete
// device types interpret Target/Interval/PayloadSize according to their own
// protocol; generic ones (see pusher.go) use them directly.
type Config struct {
	ID           string
	Type         Type
	BindIP       net.IP
	Target       string        // "ip:port" of the peer/well-known destination
	Interval     time.Duration // time between emissions
	PayloadSize  int
	Vendor, Name string
}
