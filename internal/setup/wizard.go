package setup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultVersion = "v0.5.1"

func Run() {
	version := defaultVersion
	RunWithVersion(version)
}

func RunWithVersion(version string) {
	cyan := "\033[36m"
	green := "\033[32m"
	yellow := "\033[33m"
	bold := "\033[1m"
	reset := "\033[0m"

	clearScreen()
	fmt.Printf("%s%s╔══════════════════════════════════════════════╗%s\n", cyan, bold, reset)
	fmt.Printf("%s%s║      search-mcp — One-Click Setup Wizard     ║%s\n", cyan, bold, reset)
	fmt.Printf("%s%s╚══════════════════════════════════════════════╝%s\n\n", cyan, bold, reset)

	reader := bufio.NewReader(os.Stdin)

	// ── Step 1: Select IDE/CLI ──
	ides := []struct {
		name string
		key  string
	}{
		{"Hermes Agent", "hermes"},
		{"Claude Code (CLI)", "claude-code"},
		{"Claude Desktop", "claude-desktop"},
		{"Cursor IDE", "cursor"},
		{"OpenCode CLI", "opencode"},
		{"Codex CLI / Codex IDE", "codex"},
		{"Antigravity CLI / IDE", "antigravity"},
		{"VS Code + Cline/Continue", "vscode"},
	}

	fmt.Printf("%s📌 Step 1: Which IDE/CLI will use search-mcp?%s\n\n", bold, reset)
	for i, ide := range ides {
		fmt.Printf("  %d. %s\n", i+1, ide.name)
	}
	fmt.Printf("\n  Choice [1-%d]: ", len(ides))
	choice := readInt(reader, 1, len(ides))
	selected := ides[choice-1]
	fmt.Printf("\n  ✅ Selected: %s\n\n", selected.name)

	// ── Step 2: Install binary ──
	fmt.Printf("%s📌 Step 2: Installing search-mcp binary%s\n\n", bold, reset)
	installDir := filepath.Join(os.Getenv("USERPROFILE"), ".search-mcp")
	os.MkdirAll(installDir, 0755)

	exePath, _ := os.Executable()
	destPath := filepath.Join(installDir, "search-mcp.exe")
	copyFile(exePath, destPath)
	fmt.Printf("  ✅ Installed: %s\n", destPath)
	fmt.Printf("  📝 Tip: add %s to PATH for easy access\n", installDir)

	// ── Step 3: Check Kimi WebBridge ──
	fmt.Printf("\n%s📌 Step 3: Checking Kimi WebBridge%s\n\n", bold, reset)
	kimiDaemon := filepath.Join(os.Getenv("USERPROFILE"), ".kimi-webbridge", "bin", "kimi-webbridge.exe")
	kimiOK := fileExists(kimiDaemon)
	extOK := checkKimiExtension()

	if kimiOK {
		fmt.Printf("  ✅ Kimi daemon: %s\n", kimiDaemon)
	} else {
		fmt.Printf("  %s⚠️  Kimi daemon NOT found.%s\n", yellow, reset)
		fmt.Printf("  📥 Install Kimi Desktop: https://kimi.moonshot.cn\n")
	}
	if extOK {
		fmt.Printf("  ✅ Kimi extension connected (port 10086)\n")
	} else {
		fmt.Printf("  %s⚠️  Extension not connected.%s\n", yellow, reset)
		fmt.Printf("  📥 Chrome Web Store → search \"Kimi WebBridge\"\n")
	}

	// ── Step 4: Configure MCP ──
	fmt.Printf("\n%s📌 Step 4: Configuring MCP for %s%s\n\n", bold, selected.name, reset)
	configure(selected.key, destPath)

	// ── Step 5: Test ──
	fmt.Printf("\n%s📌 Step 5: Test%s\n\n", bold, reset)
	fmt.Printf("  Chrome açık ve Kimi eklentisi aktif olmalı.\n")
	fmt.Printf("  %sTesti başlatmak için Enter...%s", yellow, reset)
	reader.ReadString('\n')

	if kimiOK && extOK {
		testBrowser()
	} else {
		fmt.Printf("\n  %s⚠️  Kimi eksik, test atlandı. Kurup tekrar: search-mcp setup%s\n", yellow, reset)
	}

	// ── Done ──
	fmt.Printf("\n%s%s╔══════════════════════════════════════════════╗%s\n", green, bold, reset)
	fmt.Printf("%s%s║          Setup Complete! 🎉                  ║%s\n", green, bold, reset)
	fmt.Printf("%s%s╚══════════════════════════════════════════════╝%s\n\n", green, bold, reset)
	fmt.Printf("  %s is now configured.\n", selected.name)
	fmt.Printf("  Cache DB: %s\n\n", filepath.Join(installDir, "cache.db"))
	fmt.Printf("  Try asking your agent:\n")
	fmt.Printf("  %s\"search for 'golang concurrency patterns'\"%s\n\n", yellow, reset)
}

// ─── MCP configuration per IDE/CLI ──────────────────────────────

func configure(key, binaryPath string) {
	escaped := strings.ReplaceAll(binaryPath, "\\", "\\\\")

	switch key {
	case "hermes":
		fmt.Printf("  Run:\n")
		fmt.Printf("  \033[33mhermes mcp add search-mcp -- stdio %s\033[0m\n", binaryPath)

	case "claude-code":
		fmt.Printf("  Run:\n")
		fmt.Printf("  \033[33mclaude mcp add search-mcp -- %s\033[0m\n", binaryPath)
		fmt.Printf("\n  Or manually edit the Claude Code project/user config.\n")

	case "claude-desktop":
		configDir := filepath.Join(os.Getenv("APPDATA"), "Claude")
		configPath := filepath.Join(configDir, "claude_desktop_config.json")
		os.MkdirAll(configDir, 0755)
		writeOrMergeJSON(configPath, "search-mcp", map[string]any{
			"command": binaryPath,
			"args":    []string{},
		})
		fmt.Printf("  ✅ Config: %s\n", configPath)

	case "cursor":
		configDir := filepath.Join(os.Getenv("USERPROFILE"), ".cursor")
		configPath := filepath.Join(configDir, "mcp.json")
		os.MkdirAll(configDir, 0755)
		writeOrMergeJSON(configPath, "search-mcp", map[string]any{
			"command": binaryPath,
		})
		fmt.Printf("  ✅ Auto-created: %s\n", configPath)

	case "opencode":
		configPath := filepath.Join(os.Getenv("USERPROFILE"), ".opencode", "opencode.json")
		os.MkdirAll(filepath.Dir(configPath), 0755)
		writeOpenCodeMCP(configPath, binaryPath)
		fmt.Printf("  ✅ Config: %s\n", configPath)

	case "codex":
		configDir := filepath.Join(os.Getenv("USERPROFILE"), ".codex")
		configPath := filepath.Join(configDir, "config.toml")
		os.MkdirAll(configDir, 0755)
		appendOrCreateTOML(configPath, "search-mcp", binaryPath)
		fmt.Printf("  ✅ Config: %s\n", configPath)
		fmt.Printf("  (or run: codex mcp add search-mcp -- %s)\n", binaryPath)

	case "antigravity":
		configDir := filepath.Join(os.Getenv("USERPROFILE"), ".gemini", "config")
		configPath := filepath.Join(configDir, "mcp_config.json")
		os.MkdirAll(configDir, 0755)
		writeOrMergeJSON(configPath, "search-mcp", map[string]any{
			"command": binaryPath,
		})
		fmt.Printf("  ✅ Config: %s\n", configPath)

	case "vscode":
		fmt.Printf("  For VS Code with Cline:\n")
		fmt.Printf("  Cline Settings → MCP Servers → Add:\n")
		fmt.Printf("  Name: search-mcp | Command: %s\n\n", binaryPath)
		fmt.Printf("  For VS Code with Continue:\n")
		fmt.Printf("  ~/.continue/config.json → experimental.mcpServers:\n")
		fmt.Printf(`  "search-mcp": {"command": "%s"}`+"\n", escaped)
	}
}

// ─── config file writers ────────────────────────────────────────

func writeOrMergeJSON(path, serverName string, serverConfig map[string]any) {
	// Read existing
	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	// Ensure mcpServers key
	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[serverName] = serverConfig
	existing["mcpServers"] = servers

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, data, 0644)
}

func writeOpenCodeMCP(path, binaryPath string) {
	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}

	mcp, _ := existing["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}

	mcp["search-mcp"] = map[string]any{
		"type":    "local",
		"command": []string{binaryPath},
		"enabled": true,
	}
	existing["mcp"] = mcp

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(path, data, 0644)
}

func appendOrCreateTOML(path, serverName, binaryPath string) {
	escaped := strings.ReplaceAll(binaryPath, "\\", "\\\\")
	entry := fmt.Sprintf("\n[mcp_servers.%s]\ncommand = \"%s\"\n", serverName, escaped)

	existing, err := os.ReadFile(path)
	if err != nil {
		os.WriteFile(path, []byte(entry), 0644)
		return
	}

	// Don't duplicate
	if strings.Contains(string(existing), fmt.Sprintf("[mcp_servers.%s]", serverName)) {
		return
	}
	os.WriteFile(path, append(existing, []byte(entry)...), 0644)
}

// ─── helpers ────────────────────────────────────────────────────

func clearScreen()            { fmt.Print("\033[H\033[2J") }
func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func copyFile(src, dst string) {
	data, _ := os.ReadFile(src)
	os.WriteFile(dst, data, 0755)
}

func readInt(r *bufio.Reader, min, max int) int {
	for {
		s, _ := r.ReadString('\n')
		s = strings.TrimSpace(s)
		var n int
		fmt.Sscanf(s, "%d", &n)
		if n >= min && n <= max {
			return n
		}
		fmt.Printf("  Enter %d-%d: ", min, max)
	}
}

func checkKimiExtension() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command(
		filepath.Join(os.Getenv("USERPROFILE"), ".kimi-webbridge", "bin", "kimi-webbridge.exe"),
		"status",
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), `"extension_connected":true`)
}

func testBrowser() {
	fmt.Printf("  🔍 Running browser test...\n")

	if _, err := exec.LookPath("curl"); err != nil {
		fmt.Printf("  ⚠️  curl not found on PATH. Skipping live test.\n")
		return
	}

	cmd := exec.Command("curl", "-s", "-X", "POST",
		"http://127.0.0.1:10086/command",
		"-H", "Content-Type: application/json",
		"-d", `{"action":"navigate","args":{"url":"https://www.google.com/search?q=hello+world&hl=en","newTab":true,"group_title":"Setup Test"},"session":"setup-test"}`,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("  ⚠️  Browser test failed: %v\n", err)
		fmt.Printf("     Is Chrome open with the Kimi WebBridge extension active?\n")
		return
	}

	// Cleanup
	exec.Command("curl", "-s", "-X", "POST",
		"http://127.0.0.1:10086/command",
		"-H", "Content-Type: application/json",
		"-d", `{"action":"close_session","args":{},"session":"setup-test"}`,
	).Run()

	fmt.Printf("  ✅ Browser navigation works!\n")
}
