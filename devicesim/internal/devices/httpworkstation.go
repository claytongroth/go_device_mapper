package devices

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// httpWorkstation replaces basicPusher for TypeWorkstation: a real
// workstation both serves and calls out to its peers over real TCP, so
// unlike camera/mic/iot (still generic UDP via basicPusher), this device
// runs an actual net/http server bound to its own IP, and a client loop
// that periodically GETs its peer's server. That gives the capture side
// genuine TCP/HTTP traffic to decode, not just UDP.
type httpWorkstation struct {
	cfg Config
}

func init() {
	Register(TypeWorkstation, func(cfg Config) Device { return &httpWorkstation{cfg: cfg} })
}

func (w *httpWorkstation) Info() Info {
	return Info{
		ID:     w.cfg.ID,
		Type:   w.cfg.Type,
		IP:     w.cfg.BindIP,
		Vendor: w.cfg.Vendor,
		Name:   w.cfg.Name,
	}
}

// Run starts this workstation's own HTTP server (so peers have something to
// call), then loops calling its configured peer (cfg.Target, "ip:port") at
// cfg.Interval until ctx is cancelled.
func (w *httpWorkstation) Run(ctx context.Context) error {
	// cfg.Target is "ip:port" — the server binds to the same port on its own
	// BindIP, so every workstation is simultaneously a server (for whoever
	// targets it) and a client (of whoever it targets).
	_, port, err := net.SplitHostPort(w.cfg.Target)
	if err != nil {
		return fmt.Errorf("httpWorkstation %s: split target %q: %w", w.cfg.ID, w.cfg.Target, err)
	}

	ln, err := net.Listen("tcp4", net.JoinHostPort(w.cfg.BindIP.String(), port))
	if err != nil {
		return fmt.Errorf("httpWorkstation %s: listen: %w", w.cfg.ID, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(rw, "hello from %s\n", w.cfg.ID)
	})
	srv := &http.Server{Handler: mux}

	// srv.Serve(ln) blocks until the listener closes; it has no way to
	// notice ctx being cancelled on its own, so this goroutine's only job is
	// to force-close the server once shutdown is signaled — same pattern as
	// runSink's ctx-watcher goroutine in main.go.
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("httpWorkstation server failed", "id", w.cfg.ID, "err", err)
		}
	}()

	// A custom Dialer with LocalAddr set forces outbound requests to
	// originate from this device's own BindIP, same reasoning as
	// basicPusher's laddr — without it, every workstation's HTTP client
	// traffic would appear to come from the default 127.0.0.1.
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				LocalAddr: &net.TCPAddr{IP: w.cfg.BindIP},
			}).DialContext,
		},
	}

	url := "http://" + w.cfg.Target + "/"
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			resp, err := client.Get(url)
			if err != nil {
				// Peer may not be listening yet (startup ordering) or may
				// have shut down first — skip this tick rather than killing
				// the whole device, same "loss is a real, measurable thing"
				// spirit as basicPusher's sequence numbers.
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}
}
