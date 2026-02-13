package world

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const brpPortOffset = 1000

// serverSessionParts is the number of hyphen-separated parts in a server
// session name: cm-server-{worldID}-{cpID}.
const serverSessionParts = 4

// trunkSessionParts is the number of hyphen-separated parts in a trunk
// session name: cm-trunk-{worldID}-{cpID}.
const trunkSessionParts = 4

// defaultTrunkPort is used when TEMPLATE_TRUNK_PORT is not set.
const defaultTrunkPort = 8081

// GameServerMode distinguishes production (release binary) from dev (cargo watch).
type GameServerMode string

const (
	GameServerModeProd GameServerMode = "prod"
	GameServerModeDev  GameServerMode = "dev"
)

// GameServer represents a game server running in a tmux session.
type GameServer struct {
	SessionName      string // tmux session: cm-server-{worldID}-{cpID}
	Port             int    // GAME_PORT
	BRPPort          int    // GAME_PORT + 1000
	WorldID          string
	CPID             string
	Mode             GameServerMode
	TrunkPort        int    // trunk serve port (0 = no trunk session)
	TrunkSessionName string // tmux session: cm-trunk-{worldID}-{cpID}
}

// IsAlive checks if the tmux session still exists.
func (s *GameServer) IsAlive() bool {
	return exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(), "tmux", "has-session", "-t", s.SessionName,
	).Run() == nil
}

// Stop kills the tmux session.
func (s *GameServer) Stop() error {
	return exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(), "tmux", "kill-session", "-t", s.SessionName,
	).Run()
}

func serverSessionName(worldID, cpID string) string {
	return "cm-server-" + worldID + "-" + cpID
}

func trunkSessionName(worldID, cpID string) string {
	return "cm-trunk-" + worldID + "-" + cpID
}

// GameServerManager manages game server processes running in tmux sessions.
// Sessions survive harness restarts; Recover() rediscovers them on startup.
type GameServerManager struct {
	mu      sync.Mutex
	servers map[string]*GameServer // key: "{worldID}/{cpID}"
	ports   *PortAllocator
	logger  *slog.Logger
	logsDir string
}

// NewGameServerManager creates a new game server manager.
func NewGameServerManager(
	logger *slog.Logger, logsDir string,
) *GameServerManager {
	return &GameServerManager{
		servers: make(map[string]*GameServer),
		ports:   NewPortAllocator(),
		logger:  logger,
		logsDir: logsDir,
	}
}

// Connect returns a running production game server for the given checkpoint,
// starting one if necessary.
func (m *GameServerManager) Connect(
	worldID, cpID, checkpointDir string,
) (*GameServer, error) {
	return m.connectMode(
		worldID, cpID, checkpointDir, GameServerModeProd,
	)
}

// ConnectDev returns a running dev game server (cargo watch) for the given
// checkpoint, starting one if necessary.
func (m *GameServerManager) ConnectDev(
	worldID, cpID, checkpointDir string,
) (*GameServer, error) {
	return m.connectMode(
		worldID, cpID, checkpointDir, GameServerModeDev,
	)
}

func (m *GameServerManager) connectMode(
	worldID, cpID, checkpointDir string,
	mode GameServerMode,
) (*GameServer, error) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return existing if alive.
	if srv, ok := m.servers[key]; ok {
		if srv.IsAlive() {
			return srv, nil
		}
		// Stale entry — clean up.
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		m.logger.Info("cleaned up stale game server",
			"key", key, "port", srv.Port)
	}

	port, err := m.ports.Allocate()
	if err != nil {
		return nil, fmt.Errorf("no available ports: %w", err)
	}

	srv, err := m.startServerTmux(
		worldID, cpID, checkpointDir, port, mode,
	)
	if err != nil {
		m.ports.Release(port)
		return nil, err
	}

	m.servers[key] = srv

	return srv, nil
}

// startServerTmux creates a tmux session and runs the game server command.
func (m *GameServerManager) startServerTmux(
	worldID, cpID, checkpointDir string,
	port int,
	mode GameServerMode,
) (*GameServer, error) {
	sessionName := serverSessionName(worldID, cpID)
	brpPort := port + brpPortOffset

	logDir := filepath.Join(m.logsDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	// Kill any stale session with the same name.
	_ = exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "kill-session", "-t", sessionName,
	).Run()

	// Create tmux session.
	createErr := exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(), "tmux", "new-session", "-d",
		"-s", sessionName,
		"-c", checkpointDir,
		"-e", fmt.Sprintf("GAME_PORT=%d", port),
		"-e", fmt.Sprintf("BRP_PORT=%d", brpPort),
		"-e", "CM_SERVER_MODE="+string(mode),
	).Run()
	if createErr != nil {
		return nil, fmt.Errorf("creating tmux session: %w", createErr)
	}

	// Set up log capture via pipe-pane.
	logFile := filepath.Join(logDir, "game-server.log")
	_ = exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "pipe-pane", "-t", sessionName, "-o",
		"cat >> "+logFile,
	).Run()

	// Choose command based on mode.
	cmd := "./target/release/server"
	if mode == GameServerModeDev {
		cmd = "cargo watch -w shared -w server -x 'run -p server'"
	}

	// Send the command.
	sendErr := exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "send-keys", "-t", sessionName, cmd, "Enter",
	).Run()
	if sendErr != nil {
		_ = exec.CommandContext( //nolint:gosec // G204: controlled args
			context.Background(),
			"tmux", "kill-session", "-t", sessionName,
		).Run()

		return nil, fmt.Errorf(
			"sending command to tmux session: %w", sendErr,
		)
	}

	srv := &GameServer{
		SessionName: sessionName,
		Port:        port,
		BRPPort:     brpPort,
		WorldID:     worldID,
		CPID:        cpID,
		Mode:        mode,
	}

	m.logger.Info("game server started",
		"worldID", worldID,
		"cpID", cpID,
		"port", port,
		"mode", mode,
		"session", sessionName,
	)

	return srv, nil
}

// GetServer returns the game server for the given world/checkpoint, or nil.
// Cleans up stale entries where the tmux session has died.
func (m *GameServerManager) GetServer(worldID, cpID string) *GameServer {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	srv, ok := m.servers[key]
	if !ok {
		return nil
	}

	if !srv.IsAlive() {
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		m.logger.Info("cleaned up dead game server",
			"key", key, "port", srv.Port)

		return nil
	}

	return srv
}

// Disconnect stops and removes a specific game server by world/checkpoint ID.
// Also kills any associated trunk serve session. No-op if the server doesn't exist.
func (m *GameServerManager) Disconnect(worldID, cpID string) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	srv, ok := m.servers[key]
	if !ok {
		return
	}

	// Kill trunk session if running.
	if srv.TrunkSessionName != "" {
		_ = exec.CommandContext( //nolint:gosec // G204: controlled args
			context.Background(),
			"tmux", "kill-session", "-t", srv.TrunkSessionName,
		).Run()
	}

	_ = srv.Stop()
	m.ports.Release(srv.Port)
	delete(m.servers, key)
	m.logger.Info("disconnected game server",
		"worldID", worldID, "cpID", cpID, "port", srv.Port)
}

// StopByWorldExcept kills all game servers for a world except the given
// checkpoint. Used when a new build completes to clean up old servers.
func (m *GameServerManager) StopByWorldExcept(worldID, keepCPID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, srv := range m.servers {
		if srv.WorldID != worldID || srv.CPID == keepCPID {
			continue
		}

		_ = srv.Stop()
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		m.logger.Info("stopped old game server",
			"worldID", worldID, "cpID", srv.CPID, "port", srv.Port)
	}
}

// Shutdown stops all running game servers and trunk sessions. Only called on
// graceful SIGINT/SIGTERM. On harness crash/restart, tmux sessions survive for
// Recover().
func (m *GameServerManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, srv := range m.servers {
		if srv.TrunkSessionName != "" {
			_ = exec.CommandContext( //nolint:gosec // G204: controlled args
				context.Background(),
				"tmux", "kill-session", "-t", srv.TrunkSessionName,
			).Run()
		}
		_ = srv.Stop()
		m.ports.Release(srv.Port)
		delete(m.servers, key)
		m.logger.Info("game server stopped (shutdown)", "key", key)
	}
}

// Recover discovers existing game server and trunk serve tmux sessions on
// startup. Session names follow the patterns cm-server-{worldID}-{cpID} and
// cm-trunk-{worldID}-{cpID}. Environment variables are read from each session.
func (m *GameServerManager) Recover() {
	out, err := exec.CommandContext(
		context.Background(),
		"tmux", "list-sessions", "-F", "#{session_name}",
	).Output()
	if err != nil {
		// tmux not running or no sessions — nothing to recover.
		m.logger.Info("no tmux sessions to recover", "error", err)

		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	// First pass: recover game server sessions.
	for _, line := range lines {
		if !strings.HasPrefix(line, "cm-server-") {
			continue
		}

		parts := strings.SplitN(line, "-", serverSessionParts)
		if len(parts) != serverSessionParts {
			m.logger.Warn("unparseable server session name",
				"name", line)

			continue
		}

		worldID := parts[2]
		cpID := parts[3]

		port, ok := m.readTmuxEnvInt(line, "GAME_PORT")
		if !ok {
			continue
		}

		mode := GameServerModeProd
		if m.readTmuxEnvStr(line, "CM_SERVER_MODE") == string(GameServerModeDev) {
			mode = GameServerModeDev
		}

		key := worldID + "/" + cpID
		m.servers[key] = &GameServer{
			SessionName: line,
			Port:        port,
			BRPPort:     port + brpPortOffset,
			WorldID:     worldID,
			CPID:        cpID,
			Mode:        mode,
		}
		m.ports.MarkInUse(port)

		m.logger.Info("recovered game server",
			"worldID", worldID,
			"cpID", cpID,
			"port", port,
			"mode", mode,
			"session", line,
		)
	}

	// Second pass: recover trunk serve sessions and attach to existing servers.
	for _, line := range lines {
		if !strings.HasPrefix(line, "cm-trunk-") {
			continue
		}

		parts := strings.SplitN(line, "-", trunkSessionParts)
		if len(parts) != trunkSessionParts {
			m.logger.Warn("unparseable trunk session name",
				"name", line)

			continue
		}

		worldID := parts[2]
		cpID := parts[3]
		key := worldID + "/" + cpID

		trunkPort, ok := m.readTmuxEnvInt(line, "TRUNK_PORT")
		if !ok {
			continue
		}

		srv, tracked := m.servers[key]
		if !tracked {
			// Orphaned trunk session — will be cleaned up by ReapOrphans.
			m.logger.Warn("trunk session has no matching server",
				"session", line)

			continue
		}

		srv.TrunkPort = trunkPort
		srv.TrunkSessionName = line

		m.logger.Info("recovered trunk serve",
			"worldID", worldID,
			"cpID", cpID,
			"trunkPort", trunkPort,
			"session", line,
		)
	}
}

// ReapOrphans scans tmux for cm-server-* and cm-trunk-* sessions not tracked
// in the manager's map and kills them. This catches sessions leaked by crashes
// or error paths.
func (m *GameServerManager) ReapOrphans() {
	out, err := exec.CommandContext(
		context.Background(),
		"tmux", "list-sessions", "-F", "#{session_name}",
	).Output()
	if err != nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, line := range strings.Split(
		strings.TrimSpace(string(out)), "\n",
	) {
		var worldID, cpID string

		switch {
		case strings.HasPrefix(line, "cm-server-"):
			parts := strings.SplitN(line, "-", serverSessionParts)
			if len(parts) != serverSessionParts {
				continue
			}
			worldID = parts[2]
			cpID = parts[3]

		case strings.HasPrefix(line, "cm-trunk-"):
			parts := strings.SplitN(line, "-", trunkSessionParts)
			if len(parts) != trunkSessionParts {
				continue
			}
			worldID = parts[2]
			cpID = parts[3]

		default:
			continue
		}

		key := worldID + "/" + cpID

		if srv, tracked := m.servers[key]; tracked {
			// For trunk sessions, verify this is the one we're tracking.
			if strings.HasPrefix(line, "cm-trunk-") &&
				srv.TrunkSessionName == line {
				continue
			}
			// For server sessions, it's tracked.
			if strings.HasPrefix(line, "cm-server-") {
				continue
			}
		}

		// Orphaned session — not in our map. Kill it.
		_ = exec.CommandContext( //nolint:gosec // G204: controlled args
			context.Background(),
			"tmux", "kill-session", "-t", line,
		).Run()
		m.logger.Info("reaped orphaned session",
			"session", line,
			"worldID", worldID,
			"cpID", cpID,
		)
	}
}

// readTmuxEnvInt reads an integer environment variable from a tmux session.
func (m *GameServerManager) readTmuxEnvInt(
	session, key string,
) (int, bool) {
	raw := m.readTmuxEnvStr(session, key)
	if raw == "" {
		return 0, false
	}

	val, err := strconv.Atoi(raw)
	if err != nil {
		m.logger.Warn("invalid tmux env value",
			"session", session, "key", key, "raw", raw, "error", err)

		return 0, false
	}

	return val, true
}

// readTmuxEnvStr reads a string environment variable from a tmux session.
// Returns empty string on failure.
func (m *GameServerManager) readTmuxEnvStr(
	session, key string,
) string {
	out, err := exec.CommandContext(
		context.Background(),
		"tmux", "show-environment", "-t", session, key,
	).Output()
	if err != nil {
		return ""
	}

	s := strings.TrimSpace(string(out))
	if eqIdx := strings.Index(s, "="); eqIdx >= 0 {
		return s[eqIdx+1:]
	}

	return ""
}

// RecoveredServers returns a snapshot of all servers in the map.
// Used by main.go to sync server_port values back to SQLite after recovery.
func (m *GameServerManager) RecoveredServers() []*GameServer {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*GameServer, 0, len(m.servers))
	for _, srv := range m.servers {
		result = append(result, srv)
	}

	return result
}

// StartTrunkServe creates a tmux session running trunk serve for the WASM
// client. The trunk port is read from TEMPLATE_TRUNK_PORT env (default 8081).
// Must be called after the game server is already tracked in the servers map.
func (m *GameServerManager) StartTrunkServe(
	worldID, cpID, checkpointDir string,
) (int, error) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	srv, ok := m.servers[key]
	if !ok {
		return 0, fmt.Errorf("no game server for %s", key)
	}

	// Already running?
	if srv.TrunkPort > 0 && srv.TrunkSessionName != "" {
		alive := exec.CommandContext( //nolint:gosec // G204: controlled args
			context.Background(), "tmux", "has-session", "-t", srv.TrunkSessionName,
		).Run() == nil
		if alive {
			return srv.TrunkPort, nil
		}
		// Stale — will recreate below.
	}

	trunkPort := defaultTrunkPort
	if envPort := os.Getenv("TEMPLATE_TRUNK_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			trunkPort = p
		}
	}

	sessionName := trunkSessionName(worldID, cpID)
	clientDir := filepath.Join(checkpointDir, "client")

	logDir := filepath.Join(m.logsDir, "worlds", worldID, cpID)
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return 0, fmt.Errorf("creating log directory: %w", err)
	}

	// Kill any stale session with the same name.
	_ = exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "kill-session", "-t", sessionName,
	).Run()

	// Create tmux session.
	createErr := exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(), "tmux", "new-session", "-d",
		"-s", sessionName,
		"-c", clientDir,
		"-e", fmt.Sprintf("TRUNK_PORT=%d", trunkPort),
	).Run()
	if createErr != nil {
		return 0, fmt.Errorf("creating trunk tmux session: %w", createErr)
	}

	// Log capture.
	logFile := filepath.Join(logDir, "trunk-serve.log")
	_ = exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "pipe-pane", "-t", sessionName, "-o",
		"cat >> "+logFile,
	).Run()

	// Send the trunk serve command.
	cmd := fmt.Sprintf(
		"trunk serve --address 0.0.0.0 --port %d", trunkPort,
	)
	sendErr := exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "send-keys", "-t", sessionName, cmd, "Enter",
	).Run()
	if sendErr != nil {
		_ = exec.CommandContext( //nolint:gosec // G204: controlled args
			context.Background(),
			"tmux", "kill-session", "-t", sessionName,
		).Run()
		return 0, fmt.Errorf("sending trunk serve command: %w", sendErr)
	}

	srv.TrunkPort = trunkPort
	srv.TrunkSessionName = sessionName

	m.logger.Info("trunk serve started",
		"worldID", worldID,
		"cpID", cpID,
		"port", trunkPort,
		"session", sessionName,
	)

	return trunkPort, nil
}

// StopTrunkServe kills the trunk serve tmux session for a checkpoint.
func (m *GameServerManager) StopTrunkServe(worldID, cpID string) {
	key := worldID + "/" + cpID
	m.mu.Lock()
	defer m.mu.Unlock()

	srv, ok := m.servers[key]
	if !ok || srv.TrunkSessionName == "" {
		return
	}

	_ = exec.CommandContext( //nolint:gosec // G204: controlled args
		context.Background(),
		"tmux", "kill-session", "-t", srv.TrunkSessionName,
	).Run()

	m.logger.Info("trunk serve stopped",
		"worldID", worldID, "cpID", cpID,
		"session", srv.TrunkSessionName)

	srv.TrunkPort = 0
	srv.TrunkSessionName = ""
}
