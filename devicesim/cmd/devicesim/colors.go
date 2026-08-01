package main

import (
	"fmt"

	"devicesim/internal/devices"
)

// ANSI escape codes for terminal text color. \033 is the escape character;
// "[36m" etc. sets the foreground color, "[0m" resets back to default.
// Virtually every terminal (and `docker compose logs`) understands these.
const (
	colorReset   = "\033[0m"
	colorCyan    = "\033[36m"
	colorMagenta = "\033[35m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorRed     = "\033[31m"
	colorGray    = "\033[90m"
)

// deviceColor assigns each device Type a distinct color so the swarm's
// startup log visually groups devices by class at a glance.
func deviceColor(t devices.Type) string {
	switch t {
	case devices.TypeCamera:
		return colorCyan
	case devices.TypeMic:
		return colorMagenta
	case devices.TypeIoT:
		return colorYellow
	case devices.TypeWorkstation:
		return colorBlue
	case devices.TypeRogue:
		return colorRed
	default:
		return colorGray
	}
}

// logDeviceStarting prints one verbose, color-coded line per device as it
// comes up: its class (colored), identity/vendor info, the address it's
// bound to, and where + how often it's sending. This is separate from the
// slog lines elsewhere (which stay plain/structured) — it exists purely so
// a human watching the swarm start can see, at a glance, what's alive and
// which class each device belongs to.
func logDeviceStarting(info devices.Info, cfg devices.Config) {
	c := deviceColor(info.Type)
	fmt.Printf("%s[%-11s]%s %-14s vendor=%-10s name=%-16s ip=%-12s target=%-18s every=%s\n",
		c, info.Type, colorReset, info.ID, info.Vendor, info.Name, info.IP, cfg.Target, cfg.Interval)
}

// logDeviceStopped prints a device's failure in red so it stands out from
// the (green-free, deliberately plain) startup lines above — an error here
// means the device's Run loop exited early, which shouldn't normally happen
// once the swarm is up.
func logDeviceStopped(info devices.Info, err error) {
	fmt.Printf("%s[%-11s]%s %-14s stopped: %v\n", colorRed, info.Type, colorReset, info.ID, err)
}
