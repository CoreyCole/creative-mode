package world

import (
	"fmt"
	"sync"
)

const (
	gameServerMinPort = 9001
	gameServerMaxPort = 9999
)

// PortAllocator manages a pool of ports for game servers.
type PortAllocator struct {
	mu      sync.Mutex
	inUse   map[int]bool
	minPort int
	maxPort int
}

// NewPortAllocator creates a port allocator for the range 9001-9999.
func NewPortAllocator() *PortAllocator {
	return &PortAllocator{
		inUse:   make(map[int]bool),
		minPort: gameServerMinPort,
		maxPort: gameServerMaxPort,
	}
}

// Allocate returns the next available port.
func (p *PortAllocator) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for port := p.minPort; port <= p.maxPort; port++ {
		if !p.inUse[port] {
			p.inUse[port] = true

			return port, nil
		}
	}

	return 0, fmt.Errorf("no available ports in range %d-%d", p.minPort, p.maxPort)
}

// Release returns a port to the available pool.
func (p *PortAllocator) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inUse, port)
}

// MarkInUse marks a port as in-use without allocating it.
// Used during recovery to reserve ports for existing tmux sessions.
func (p *PortAllocator) MarkInUse(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inUse[port] = true
}
