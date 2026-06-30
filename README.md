# 🔍 search-mcp

**Free, unlimited Google web search for AI agents.** Uses your real Chrome browser (with cookies/sessions) via Kimi WebBridge. No API keys. No rate limits.

```
Agent: "search for 'golang concurrency patterns'"
  → Google → 24 results → Auto-fetch all pages → Clean markdown → Agent
```

## ⚡ One-Liner Install

### Windows (PowerShell)
```powershell
irm https://raw.githubusercontent.com/menesekinci/search-mcp/main/install.ps1 | iex
```

### macOS / Linux
```bash
curl -fsSL https://raw.githubusercontent.com/menesekinci/search-mcp/main/install.sh | bash
```

### Go install
```bash
go install github.com/menesekinci/search-mcp@latest
search-mcp setup
```

## 🧠 Supported IDEs/CLIs

| IDE/CLI | Auto-Config |
|---------|:-----------:|
| Hermes Agent | ✅ |
| Claude Code | ✅ |
| Claude Desktop | ✅ |
| Cursor IDE | ✅ |
| OpenCode CLI | ✅ |
| Codex CLI / IDE | ✅ |
| Antigravity CLI / IDE | ✅ |
| VS Code (Cline/Continue) | ✅ |

`search-mcp setup` auto-creates the MCP config for any of these.

## 🚀 Levels

| Level | Results | Use case |
|-------|---------|----------|
| `low` | 6 | Quick fact check |
| `medium` | 12 | Normal research (default) |
| `high` | 24 | Deep literature scan |
| `crazy` | 48 | Exhaustive discovery |

```json
{"query": "rust async patterns", "level": "high", "site": "docs.rs"}
```

## 🔧 How it works

```
Agent → MCP stdio → search-mcp.exe → Kimi WebBridge → Chrome → Google
                                              ↓
                            go-readability + html-to-markdown
                                              ↓
                                     Clean markdown → Agent
```

- **Pagination:** auto-paginates Google until target result count is hit
- **Dedup:** URL-based within a search, same page never fetched twice
- **Always live:** no cache, no local database — every call hits Google and the target pages fresh. True to the research spirit.
- **Multi-query:** `queries: [...]` runs each query sequentially in its own Chrome tab (with a short randomized delay between them to avoid bot detection). Within a single query, the top results are fetched concurrently (up to 3 at a time).

## 📦 Tools

| Tool | Description |
|------|-------------|
| `web_search` | Search + auto-fetch. One call = everything. |
| `fetch_page` | Fetch specific URL (user pastes a link). |

## 🛠️ CLI

```bash
search-mcp setup       # Interactive setup wizard
search-mcp --version   # v0.6.0
search-mcp             # MCP server (stdio mode)
```

## 📋 Requirements

- **Chrome** (any recent version)
- **Kimi WebBridge** extension or Kimi Desktop (for browser automation)
- Windows / macOS / Linux

## 🔗 Links

- [Kimi WebBridge](https://kimi.moonshot.cn) — browser automation daemon
- [MCP Specification](https://modelcontextprotocol.io/) — Model Context Protocol
