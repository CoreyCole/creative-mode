package world

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const gameServerGracePeriod = 2 * time.Minute

// GameServer represents a running game server process.
type GameServer struct {
	Cmd     *exec.Cmd
	Port    int
	WorldID string
	CPID    string
}

// GameServerManager manages the lifecycle of game server processes with
// reference counting. Multiple users on the same checkpoint share one server.
type GameServerManager struct {
	mu       sync.Mutex
	servers  map[string]*GameServer // key: "{worldID}/{cpID}"
	refCount map[string]int
	ports    *PortAllocator
	logger   *slog.Logger
	logsDir  string
}

// NewGameServerManager creates a new game server manager.
func NewGameServerManager(logger *slog.Logger, logsDir string) *GameServerManager {
	return &GameServerManager{
		servers:  make(map[string]*GameServer),
		refCount: make(map[string]int),
		ports:    NewPortAllocator(),
		logger:   logger,
		logsDir:  logsDir,
	}
}

// Connect returns a running game server for the given checkpoint, starting
// one if necessary. Increments the reference count.
func (m *GameServerManager) Connect(
	worldID, cpID, checkpointDir string,
) (*GameServer, error) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	if srv, ok := m.servers[key]; ok {
		m.refCount[key]++

		return srv, nil
	}

	port, err := m.ports.Allocate()
	if err != nil {
		return nil, fmt.Errorf("no available ports: %w", err)
	}

	srv, err := m.startServer(worldID, cpID, checkpointDir, port)
	if err != nil {
		m.ports.Release(port)

		return nil, err
	}

	m.servers[key] = srv
	m.refCount[key] = 1

	return srv, nil
}

// Disconnect decrements the reference count for a game server. If it reaches
// zero, the server is stopped after a grace period.
func (m *GameServerManager) Disconnect(worldID, cpID string) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.refCount[key] <= 0 {
		return
	}

	m.refCount[key]--
	if m.refCount[key] == 0 {
		go m.stopAfterDelay(key, gameServerGracePeriod)
	}
}

func (m *GameServerManager) startServer(
	worldID, cpID, checkpointDir string,
	port int,
) (*GameServer, error) {
	serverBin := filepath.Join(checkpointDir, "target", "release", "server")

	logDir := filepath.Join(m.logsDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.Create( //nolint:gosec // G304: internal log path
		filepath.Join(logDir, "game-server.jsonl"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating log file: %w", err)
	}

	writer := &jsonlLineWriter{
		file:    logFile,
		worldID: worldID,
		cpID:    cpID,
		event:   "game_server.output",
	}

	cmd := exec.CommandContext( //nolint:gosec // G204: internal binary path
		context.Background(), serverBin,
	)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GAME_PORT=%d", port))
	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()

		return nil, fmt.Errorf("starting game server: %w", err)
	}

	srv := &GameServer{Cmd: cmd, Port: port, WorldID: worldID, CPID: cpID}

	// Monitor for crashes.
	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			m.logger.Error(
				"game server exited",
				"worldID", worldID,
				"cpID", cpID,
				"error", waitErr,
			)
		}

		_ = logFile.Close()

		// Clean up crashed server entry.
		key := worldID + "/" + cpID
		m.mu.Lock()
		defer m.mu.Unlock()

		existing, ok := m.servers[key]
		if !ok || existing != srv {
			return
		}

		m.ports.Release(srv.Port)
		delete(m.servers, key)
		delete(m.refCount, key)
		m.logger.Info("cleaned up crashed game server",
			"key", key, "port", srv.Port)
	}()

	m.logger.Info("game server started", "worldID", worldID, "cpID", cpID, "port", port)

	return srv, nil
}

func (m *GameServerManager) stopAfterDelay(key string, delay time.Duration) {
	time.Sleep(delay)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.refCount[key] > 0 {
		return
	}

	srv, ok := m.servers[key]
	if !ok {
		return
	}

	_ = srv.Cmd.Process.Kill()
	m.ports.Release(srv.Port)
	delete(m.servers, key)
	delete(m.refCount, key)
	m.logger.Info("game server stopped", "key", key)
}

// Shutdown stops all running game servers immediately.
func (m *GameServerManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, srv := range m.servers {
		_ = srv.Cmd.Process.Kill()
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		m.logger.Info("game server stopped (shutdown)", "key", key)
	}
}

// jsonlLineWriter wraps output into structured JSONL entries.
type jsonlLineWriter struct {
	file    *os.File
	worldID string
	cpID    string
	event   string
}

func (w *jsonlLineWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		entry, marshalErr := json.Marshal(map[string]any{
			"ts":      time.Now().UTC().Format(time.RFC3339),
			"level":   "info",
			"event":   w.event,
			"worldID": w.worldID,
			"cpID":    w.cpID,
			"line":    line,
		})
		if marshalErr != nil {
			continue
		}

		_, _ = w.file.Write(append(entry, '\n'))
	}

	return len(p), nil
}
