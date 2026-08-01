package graph

import (
	"apiserver/internal/decode"
	"net"
	"sync"
)

// maxEdgeHistory caps how many transactions an Edge keeps — "last X by
// count" — so edge history stays bounded no matter how long the swarm
// runs, instead of growing forever.
const maxEdgeHistory = 50

type Edge struct {
	from         net.IP
	to           net.IP
	Transactions []decode.PacketData
}

// record appends packet to e's transaction history, dropping the oldest
// entry once history exceeds maxEdgeHistory — the capped-slice,
// drop-oldest pattern instead of a true ring buffer.
func (e *Edge) record(packet decode.PacketData) {
	e.Transactions = append(e.Transactions, packet)
	if len(e.Transactions) > maxEdgeHistory {
		e.Transactions = e.Transactions[len(e.Transactions)-maxEdgeHistory:]
	}
}

type Node struct {
	IP        net.IP
	ID        string
	Neighbors map[string]*Edge
}

type Store struct {
	mu    sync.RWMutex
	nodes map[string]*Node
}

func NewStore() *Store {
	return &Store{nodes: make(map[string]*Node)}
}

// declare a function on the Store struct itself. ip is threaded through
// now too — the previous version only ever set Node.ID, so Node.IP was
// never actually populated on creation.
func (s *Store) getOrCreate(id string, ip net.IP) *Node {
	n, ok := s.nodes[id]
	if !ok {
		n = &Node{ID: id, IP: ip, Neighbors: make(map[string]*Edge)}
		s.nodes[id] = n
	}
	return n
}

// function on the store Struct AddEdge records a transaction between from
// and to. Both directions share the same *Edge — "the relationship
// between these two IPs" is one thing, not two independent ones — and
// net.IP.String() is what turns an IP into the string key Node/Neighbors
// are keyed by.
func (s *Store) AddEdge(from, to net.IP, packet decode.PacketData) {
	// One lock for the whole method, not per-step: get-or-create both
	// nodes, check-and-create their edge, and record the transaction all
	// need to happen as one atomic unit — locking/unlocking between steps
	// would let another goroutine interleave partway through. getOrCreate
	// below must NOT take its own lock, since it's called from here while
	// this lock is already held — sync.RWMutex isn't reentrant, so locking
	// twice from the same goroutine would deadlock.
	s.mu.Lock()
	defer s.mu.Unlock()

	fromID := from.String()
	toID := to.String()

	nodeFrom := s.getOrCreate(fromID, from)
	nodeTo := s.getOrCreate(toID, to)

	edge, ok := nodeFrom.Neighbors[toID]
	if !ok {
		edge = &Edge{from: from, to: to}
		nodeFrom.Neighbors[toID] = edge
		nodeTo.Neighbors[fromID] = edge
	}
	edge.record(packet)
}

// function on the store Struct: Neighbors returns everyone a given node directly knows — one hop only (for now)
func (s *Store) Neighbors(id string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	if !ok {
		return nil
	}
	var out []string
	for neighborID := range n.Neighbors {
		out = append(out, neighborID)
	}
	return out
}

// EdgeSnapshot is a JSON-friendly view of one Edge, from one particular
// node's perspective. Edge.from/to are unexported anyway, and redundant
// here — Neighbor already says who the other end is — so this only
// surfaces what's actually useful: who the neighbor is and the
// transaction history between them. Exported (unlike Edge itself) since
// callers outside this package (the api package) need to reference the
// type directly now that Store hands back plain values instead of
// pre-marshaled JSON bytes — encoding is the api package's job, not
// graph's (see writeJSON).
type EdgeSnapshot struct {
	Neighbor     string              `json:"neighbor"`
	Transactions []decode.PacketData `json:"transactions"`
}

// NodeSnapshot is a JSON-friendly view of one Node — Node's own fields
// aren't quite JSON-ready as-is (net.IP needs .String() to become
// readable text instead of a raw byte slice), so this is a separate,
// deliberately-exported shape rather than marshaling Node directly.
type NodeSnapshot struct {
	ID        string         `json:"id"`
	IP        string         `json:"ip"`
	Neighbors []string       `json:"neighbors"`
	Edges     []EdgeSnapshot `json:"edges"`
}

// Snapshot returns a point-in-time view of every node currently in the
// store, each with its neighbors and the edge (including transaction
// history) connecting it to each of them. Returns plain data, not JSON —
// encoding to JSON is the api package's job (see api.writeJSON), not
// this package's.
func (s *Store) Snapshot() []NodeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := make([]NodeSnapshot, 0, len(s.nodes))
	for _, n := range s.nodes {
		neighbors := make([]string, 0, len(n.Neighbors))
		edges := make([]EdgeSnapshot, 0, len(n.Neighbors))
		// range over a map[string]*Edge gives back both the key
		// (neighborID) and the value (the *Edge itself) — no separate
		// lookup needed to get from "which neighbor" to "its Edge".
		for neighborID, edge := range n.Neighbors {
			neighbors = append(neighbors, neighborID)
			edges = append(edges, EdgeSnapshot{
				Neighbor:     neighborID,
				Transactions: edge.Transactions,
			})
		}
		snapshot = append(snapshot, NodeSnapshot{
			ID:        n.ID,
			IP:        n.IP.String(),
			Neighbors: neighbors,
			Edges:     edges,
		})
	}

	return snapshot
}

// GetAllNodes returns every node in the store, without neighbor/edge
// detail — plain data, same reasoning as Snapshot.
func (s *Store) GetAllNodes() []NodeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make([]NodeSnapshot, 0, len(s.nodes))
	for _, n := range s.nodes {
		snapshot = append(snapshot, NodeSnapshot{
			ID: n.ID,
			IP: n.IP.String(),
		})
	}
	return snapshot
}
