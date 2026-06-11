# Test: CloseThread feature
# Verifies that after web_search, the created tab is closed.
# search-mcp uses a fixed session name "search-mcp" — we must use the same.

$ErrorActionPreference = "Stop"
$binary = Join-Path (Split-Path $PSScriptRoot -Parent) "search-mcp.exe"
$session = "search-mcp"  # fixed name in main.go:104

if (-not (Test-Path $binary)) { throw "Binary not found: $binary" }

# Clean up any leftover tabs from previous test runs
$cs = @{action="close_session"; args=@{}; session=$session} | ConvertTo-Json -Compress
try { Invoke-RestMethod -Uri "http://127.0.0.1:10086/command" -Method Post -ContentType "application/json" -Body $cs -ErrorAction Stop | Out-Null } catch {}

Write-Host "=== Tabs BEFORE test (should be 0) ===" -ForegroundColor Cyan
$listReq = @{action="list_tabs"; args=@{}; session=$session} | ConvertTo-Json -Compress
$r = Invoke-RestMethod -Uri "http://127.0.0.1:10086/command" -Method Post -ContentType "application/json" -Body $listReq
Write-Host "  tabs: $($r.data.tabs.Count)"

Write-Host "`n=== Starting search-mcp.exe and triggering web_search ===" -ForegroundColor Cyan
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $binary
$psi.UseShellExecute = $false
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$proc = [System.Diagnostics.Process]::Start($psi)

# Send initialize
$proc.StandardInput.WriteLine('{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}')
$proc.StandardInput.Flush()
Start-Sleep -Milliseconds 300

# Send tools/call web_search with low level
$call = '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"web_search","arguments":{"query":"close tab feature test","level":"low"}}}'
$proc.StandardInput.WriteLine($call)
$proc.StandardInput.Flush()

# Wait for response
Write-Host "  Waiting for web_search to complete (up to 90s)..." -ForegroundColor Yellow
$deadline = (Get-Date).AddSeconds(90)
$gotResponse = $false
while (-not $proc.StandardOutput.EndOfStream -and (Get-Date) -lt $deadline) {
    if ($proc.StandardOutput.Peek() -ne -1 -or $proc.WaitForExit(200)) {
        if ($proc.StandardOutput.Peek() -ne -1) {
            $line = $proc.StandardOutput.ReadLine()
            try {
                $obj = $line | ConvertFrom-Json -ErrorAction Stop
                if ($obj.id -eq 3) {
                    $title = if ($obj.result.content) { $obj.result.content[0].text.Substring(0, [Math]::Min(100, $obj.result.content[0].text.Length)) } else { "(no content)" }
                    Write-Host "  Got web_search response: $title..." -ForegroundColor Green
                    $gotResponse = $true
                    break
                }
            } catch {}
        }
    }
}

if (-not $gotResponse) { Write-Host "  ⚠️ Timed out waiting for response" -ForegroundColor Yellow }

# Give a moment for the close_tab to settle
Start-Sleep -Seconds 1

# Kill the search-mcp process
if (-not $proc.HasExited) {
    $proc.Kill()
    $proc.WaitForExit(2000) | Out-Null
}

# Show stderr
$stderr = $proc.StandardError.ReadToEnd()
Write-Host "`n=== search-mcp stderr (last 25 lines) ===" -ForegroundColor Cyan
$stderr -split "`n" | Where-Object { $_ -match "close|thread" -or $_ -match "^\[search-mcp\]" } | Select-Object -Last 25 | ForEach-Object { Write-Host "  $_" }

# Verify
Write-Host "`n=== Tabs AFTER test (should be 0) ===" -ForegroundColor Cyan
Start-Sleep -Milliseconds 500
$r = Invoke-RestMethod -Uri "http://127.0.0.1:10086/command" -Method Post -ContentType "application/json" -Body $listReq
$count = $r.data.tabs.Count
Write-Host "  tabs: $count"
if ($count -eq 0) {
    Write-Host "`n✅ PASS: Tab was closed after web_search" -ForegroundColor Green
} else {
    Write-Host "`n❌ FAIL: $count tab(s) still open" -ForegroundColor Red
    $r.data.tabs | ForEach-Object { Write-Host "     - $($_.title) [$($_.url)]" }
}
