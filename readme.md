# Mapping Networks and Exposing an API for them in Go

A Go project that simulates a small network of devices, passively captures
and decodes their traffic, and serves the resulting network graph over a
read API — all running in a real Linux network namespace via Docker
Compose. Not realistic, but a good learning exercise.

(This project was built with the disciplined & limited use of AI tool, primarily as a challenge.)


## How it works

Two independent Go modules, tied together for local dev via `go.work`:

- **`devicesim`** simulates a randomized swarm of 1–500 virtual devices (TCP and UDP stuff only for now)
  (weighted like a real office network: mostly workstations, a minority of
  cameras/mics/IoT sensors), each bound to its own address on Linux's free
  `127.0.0.0/8` loopback range. Cameras and mics speak real RTP (RFC 3550),
  IoT sensors speak real CoAP (RFC 7252), and workstations run genuine HTTP
  servers/clients talking to each other in a peer ring — all real wire
  protocols, not simulated stand-ins.
- **`apiserver`** is the collector: it captures that traffic with
  `gopacket`/`libpcap`, decodes each packet's protocol, folds the result
  into an in-memory, mutex-guarded graph (nodes = IPs, edges = who's talked
  to whom, with a rolling history of recent transactions per edge), and
  serves it over a small HTTP API.

```
capture (pcap)  →  decode (RTP/CoAP/HTTP)  →  graph store  →  HTTP API
```

Both services run in the same Docker network namespace, so `apiserver` sees
`devicesim`'s traffic directly — no packet ever touches a physical NIC.

## Running it

```
scripts/dev.sh up      # build + run both services, streaming logs
scripts/dev.sh down    # stop and tear down
```

or directly with `docker compose up --build`. The API is published on
`localhost:8080`.

## API

| Endpoint       | Description                                                             |
|----------------|--------------------------------------------------------------------------|
| `GET /hello`   | Health-check-style hello world                                          |
| `GET /graph`   | Every node, its neighbors, and each edge's transaction history, as JSON |
| `GET /devices` | Every node (IP + ID), as JSON                                           |

Every endpoint is rate-limited to 10 requests/second per client IP (shared
budget across all routes, `429` once exceeded) and logs method/path/duration
per request.

## Project layout

```
devicesim/
  cmd/devicesim/        # launcher + randomized swarm generation
  internal/devices/      # device types, registry, RTP/CoAP/HTTP behavior
apiserver/
  cmd/apiserver/         # entrypoint
  internal/
    capture/              # pcap capture loop
    decode/               # per-protocol decoders
    graph/                 # in-memory graph store
    api/                    # HTTP routes, handlers, middleware
docker-compose.yml
scripts/dev.sh
```

## Status
Lots to do, but just learning for now!