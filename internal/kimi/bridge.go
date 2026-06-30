package kimi

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// daemonAddr is the local address the Kimi WebBridge daemon listens on.
// It must match BaseURL's host:port.
const daemonAddr = "127.0.0.1:10086"

// IsBridgeUp reports whether the Kimi WebBridge daemon is accepting TCP
// connections on its local port. A raw dial is used (no HTTP request and no
// proxy) so it is cheap and never needs a valid command payload.
func IsBridgeUp(timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", daemonAddr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DaemonPath returns the expected Kimi WebBridge daemon executable path for the
// current OS and whether that file exists. The daemon ships with Kimi Desktop
// and installs under ~/.kimi-webbridge/bin.
func DaemonPath() (path string, exists bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	name := "kimi-webbridge"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path = filepath.Join(home, ".kimi-webbridge", "bin", name)
	_, statErr := os.Stat(path)
	return path, statErr == nil
}

// StartBridge launches the Kimi WebBridge daemon if it is not already up, then
// waits up to timeout for the port to start accepting connections. It returns
// the daemon path it used (when known). It is a no-op that returns (path,nil)
// when the daemon is already running.
//
// The daemon is started detached: its stdio is left nil so Go connects it to
// the null device (never inheriting search-mcp's MCP pipe), and we do not Wait
// on it, so it outlives this process.
func StartBridge(timeout time.Duration) (string, error) {
	if IsBridgeUp(500 * time.Millisecond) {
		return "", nil // already running
	}

	path, exists := DaemonPath()
	if !exists {
		return path, fmt.Errorf("Kimi WebBridge daemon not found at %s — install Kimi Desktop from https://kimi.moonshot.cn", path)
	}

	cmd := exec.Command(path)
	if err := cmd.Start(); err != nil {
		return path, fmt.Errorf("launch %s: %w", path, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if IsBridgeUp(500 * time.Millisecond) {
			return path, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return path, fmt.Errorf("daemon launched (%s) but port %s did not come up within %s", path, daemonAddr, timeout)
}
