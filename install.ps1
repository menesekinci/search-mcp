# search-mcp Windows Installer
# One-liner: irm https://.../install.ps1 | iex
# Or: .\install.ps1

param(
    [switch]$SkipKimi,
    [switch]$Silent
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$BOLD = [char]27 + "[1m"
$GREEN = [char]27 + "[32m"
$YELLOW = [char]27 + "[33m"
$CYAN = [char]27 + "[36m"
$RED = [char]27 + "[31m"
$RESET = [char]27 + "[0m"

$VERSION = "v0.5.2"
$REPO = "menesekinci/search-mcp"
$INSTALL_DIR = "$env:USERPROFILE\.search-mcp"
$BINARY = "$INSTALL_DIR\search-mcp.exe"
$KIMI_DAEMON = "$env:USERPROFILE\.kimi-webbridge\bin\kimi-webbridge.exe"
$KIMI_URL = "https://kimi.moonshot.cn"

# ═══════════════════════════════════════════════════════════════════
# Header
# ═══════════════════════════════════════════════════════════════════

Clear-Host
Write-Host "$CYAN$BOLD╔══════════════════════════════════════════════════════╗$RESET"
Write-Host "$CYAN$BOLD║     search-mcp — One-Click Windows Installer         ║$RESET"
Write-Host "$CYAN$BOLD║     $VERSION                                          ║$RESET"
Write-Host "$CYAN$BOLD╚══════════════════════════════════════════════════════╝$RESET"
Write-Host ""
Write-Host "  Free unlimited Google web search for AI agents"
Write-Host "  Works with: Hermes, Claude, Cursor, Codex, OpenCode, Antigravity"
Write-Host ""

# ═══════════════════════════════════════════════════════════════════
# Step 1: Check prerequisites
# ═══════════════════════════════════════════════════════════════════

Write-Host "$BOLD📌 Step 1/5: Checking prerequisites$RESET"
Write-Host ""

# Chrome
$chromePath = Get-Command chrome -ErrorAction SilentlyContinue
if (-not $chromePath) {
    $chromePath = "$env:ProgramFiles\Google\Chrome\Application\chrome.exe"
}
if (-not (Test-Path $chromePath)) {
    $chromePath = "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe"
}
if (Test-Path $chromePath) {
    Write-Host "  ✅ Google Chrome found: $chromePath"
} else {
    Write-Host "  $RED❌ Google Chrome NOT found!$RESET"
    Write-Host "     Chrome is required for browser automation."
    Write-Host "     Download: https://www.google.com/chrome/"
    exit 1
}

# curl (needed for Kimi test, comes with Windows 10+)
$curlOk = Get-Command curl -ErrorAction SilentlyContinue
if ($curlOk) {
    Write-Host "  ✅ curl available (for connectivity tests)"
} else {
    Write-Host "  $YELLOW⚠️  curl not found (test feature limited)$RESET"
}

Write-Host ""

# ═══════════════════════════════════════════════════════════════════
# Step 2: Download & install search-mcp binary
# ═══════════════════════════════════════════════════════════════════

Write-Host "$BOLD📌 Step 2/5: Installing search-mcp binary$RESET"
Write-Host ""

New-Item -ItemType Directory -Force -Path $INSTALL_DIR | Out-Null

$downloadUrl = "https://github.com/$REPO/releases/download/$VERSION/search-mcp-windows-amd64.exe"

Write-Host "  📥 Downloading: $downloadUrl"
try {
    Invoke-WebRequest -Uri $downloadUrl -OutFile $BINARY -UseBasicParsing
    Write-Host "  ✅ Installed: $BINARY ($((Get-Item $BINARY).Length / 1MB) MB)"
} catch {
    Write-Host "  $YELLOW⚠️  Download failed — using local binary if available$RESET"
    # Fallback: copy from current directory if running from release
    if (Test-Path ".\search-mcp.exe") {
        Copy-Item ".\search-mcp.exe" $BINARY -Force
        Write-Host "  ✅ Copied local binary to $BINARY"
    } else {
        Write-Host "  $RED❌ No binary found. Build from source: go install github.com/$REPO@latest$RESET"
        exit 1
    }
}

# Add to PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$INSTALL_DIR*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$INSTALL_DIR", "User")
    $env:Path = "$env:Path;$INSTALL_DIR"
    Write-Host "  ✅ Added to PATH: $INSTALL_DIR"
} else {
    Write-Host "  ✅ Already in PATH"
}

Write-Host ""

# ═══════════════════════════════════════════════════════════════════
# Step 3: Kimi WebBridge
# ═══════════════════════════════════════════════════════════════════

if (-not $SkipKimi) {
    Write-Host "$BOLD📌 Step 3/5: Checking Kimi WebBridge$RESET"
    Write-Host ""

    # Check daemon
    if (Test-Path $KIMI_DAEMON) {
        Write-Host "  ✅ Kimi daemon: $KIMI_DAEMON"
        
        # Check if running + extension connected
        $status = & $KIMI_DAEMON status 2>$null | ConvertFrom-Json
        if ($status.running) {
            Write-Host "  ✅ Daemon running (port $($status.port), v$($status.version))"
            if ($status.extension_connected) {
                Write-Host "  ✅ Chrome extension connected!"
            } else {
                Write-Host "  $YELLOW⚠️  Extension NOT connected.$RESET"
                Write-Host "     Install from Chrome Web Store: search 'Kimi WebBridge'"
                Write-Host "     Or install Kimi Desktop: $KIMI_URL"
            }
        } else {
            Write-Host "  $YELLOW⚠️  Daemon not running. Starting...$RESET"
            Start-Process $KIMI_DAEMON -WindowStyle Hidden
            Start-Sleep -Seconds 2
            Write-Host "  ✅ Daemon started"
        }
    } else {
        Write-Host "  $YELLOW⚠️  Kimi WebBridge daemon NOT found.$RESET"
        Write-Host ""
        Write-Host "  ┌─────────────────────────────────────────────────────┐"
        Write-Host "  │  To install Kimi WebBridge:                        │"
        Write-Host "  │                                                     │"
        Write-Host "  │  Option A: Kimi Desktop (includes daemon)           │"
        Write-Host "  │    → $KIMI_URL                                     │"
        Write-Host "  │                                                     │"
        Write-Host "  │  Option B: Chrome extension only                    │"
        Write-Host "  │    → Chrome Web Store → 'Kimi WebBridge'            │"
        Write-Host "  │                                                     │"
        Write-Host "  │  After installing, re-run: search-mcp setup         │"
        Write-Host "  └─────────────────────────────────────────────────────┘"
        Write-Host ""
        
        if (-not $Silent) {
            Write-Host "  Press Enter to continue (you can install Kimi later)..."
            Read-Host
        }
    }

    Write-Host ""
}

# ═══════════════════════════════════════════════════════════════════
# Step 4: MCP Configuration
# ═══════════════════════════════════════════════════════════════════

Write-Host "$BOLD📌 Step 4/5: Configuring MCP$RESET"
Write-Host ""

# Run the setup wizard
$setupCmd = "$BINARY setup"
Write-Host "  Running: $setupCmd"
Write-Host ""

$env:Path = "$env:Path;$INSTALL_DIR"
$process = Start-Process -FilePath $BINARY -ArgumentList "setup" -Wait -NoNewWindow -PassThru

Write-Host ""

# ═══════════════════════════════════════════════════════════════════
# Step 5: Test
# ═══════════════════════════════════════════════════════════════════

Write-Host "$BOLD📌 Step 5/5: Quick test$RESET"
Write-Host ""

if ((Test-Path $KIMI_DAEMON) -and (Get-Command curl -ErrorAction SilentlyContinue)) {
    $testResult = & curl -s -X POST "http://127.0.0.1:10086/command" `
        -H "Content-Type: application/json" `
        -d '{"action":"navigate","args":{"url":"https://www.google.com/search?q=hello+world&hl=en","newTab":true,"group_title":"Installer Test"},"session":"installer-test"}' 2>$null
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "  ✅ Browser navigation test PASSED"
        # Cleanup
        & curl -s -X POST "http://127.0.0.1:10086/command" `
            -H "Content-Type: application/json" `
            -d '{"action":"close_session","args":{},"session":"installer-test"}' 2>$null | Out-Null
    } else {
        Write-Host "  $YELLOW⚠️  Test failed (Chrome may not be open)$RESET"
    }
} else {
    Write-Host "  ⏭️  Skipped (Kimi or curl not available)"
}

Write-Host ""

# ═══════════════════════════════════════════════════════════════════
# Done
# ═══════════════════════════════════════════════════════════════════

Write-Host "$GREEN$BOLD╔══════════════════════════════════════════════════════╗$RESET"
Write-Host "$GREEN$BOLD║              Installation Complete! 🎉              ║$RESET"
Write-Host "$GREEN$BOLD╚══════════════════════════════════════════════════════╝$RESET"
Write-Host ""
Write-Host "  Binary:    $BINARY"
Write-Host "  Cache DB:  $INSTALL_DIR\cache.db"
Write-Host "  Setup:     search-mcp setup"
Write-Host "  Version:   search-mcp --version"
Write-Host ""
Write-Host "  $YELLOW✨ Try asking your AI agent:$RESET"
Write-Host "     'search for latest AI research papers'"
Write-Host "     'find github.com mcp servers'"
Write-Host ""
