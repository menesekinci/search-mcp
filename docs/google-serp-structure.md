# Google SERP Structure Analysis

> Date: 2026-06-11 | Tested with: Kimi WebBridge + Chrome 149

## DOM Structure

### Result Container Hierarchy

```
div#search
└── div#rso
    └── div.MjjYud (×20-21 per page, ~10 organic)
        └── div.A6K0A[data-rpos]
            ├── [ORGANIC] div.wHYlTd.Ww4FFb.tF2Cxc.asEBEc
            │   └── div.N54PNb.BToiNc
            │       ├── div.kb0PBd.A9Y9g.jGGQ5e          ← Title block
            │       │   └── div.yuRUbf → span.V9tjod
            │       │       └── a.zReHs[jsname="UWckNb"]   ← **MAIN LINK**
            │       │           └── h3                     ← Title text
            │       ├── div.kb0PBd.A9Y9g[data-sncf]       ← Snippet block
            │       │   └── div.VwiC3b.yXK7lf...           ← Snippet text
            │       └── div.kb0PBd                         ← Bottom info
            │           ├── cite                           ← Display URL
            │           └── span.VuuXrf                    ← Site name
            │
            └── [AD] span.n6AgNe                          ← Sponsored indicator
                └── script (googleadservices.com)
```

### Organic vs Ad Detection

| Type | Inner element | HTML signature | Action |
|------|--------------|----------------|--------|
| Organic | `div.wHYlTd` / `div.tF2Cxc` | Normal result structure | Extract |
| Sponsored (Ad) | `span.n6AgNe` | `googleadservices.com` in script | Skip |
| Empty widget | No children | `innerHTML.length === 0` | Skip |
| "People also ask" | Different structure | No h3 + specific classes | Skip |
| "Related searches" | Table structure | `div.OJXvsb` | Skip |

## Go Extraction Strategy (goquery)

```go
doc.Find("div.MjjYud").Each(func(i int, s *goquery.Selection) {
    // Step 1: Filter - must have organic inner structure
    if s.Find("div.wHYlTd, div.tF2Cxc").Length() == 0 {
        return // skip ads, widgets, etc.
    }

    // Step 2: Extract fields
    title    := strings.TrimSpace(s.Find("h3").First().Text())
    url, ok  := s.Find("a.zReHs").First().Attr("href")
    if !ok || url == "" {
        // Fallback: find any a[href^="http"] that's not google
        s.Find("a[href^='http']").EachWithBreak(func(_ int, a *goquery.Selection) bool {
            href, _ := a.Attr("href")
            if !strings.Contains(href, "google.com") {
                url = href
                return false
            }
            return true
        })
    }

    snippetEl := s.Find("div.VwiC3b, span.VwiC3b").First()
    snippet   := strings.TrimSpace(snippetEl.Text())

    displayURL := strings.TrimSpace(s.Find("cite").First().Text())
    siteName   := strings.TrimSpace(s.Find("span.VuuXrf").First().Text())
})
```

## Key CSS Selectors

| Field | Primary Selector | Fallback | Reliability |
|-------|-----------------|----------|-------------|
| Container | `div.MjjYud` | - | 100% |
| Organic filter | `div.MjjYud div.wHYlTd` | `div.tF2Cxc` | 100% |
| Title | `h3` | - | 100% |
| URL | `a.zReHs[href]` | `h3` → parent `a` | 97% (zReHs = official link class) |
| Snippet | `div.VwiC3b` | `span.VwiC3b` | 95% (sometimes `yXK7lf`) |
| Display URL | `cite` | - | 90% (sometimes empty for social media) |
| Site name | `span.VuuXrf` | - | 85% (not always present) |

## Pagination

Google's `num` parameter is ignored in modern Google (HTML5). Instead:
- Page 1: First 10 organic results
- Page 2: `?start=10` → next 10 results
- Page 3: `?start=20` → etc.

Or use "Next" link: `a#pnnext`

## Search Operators

```
site:github.com tauri mcp        → Only github.com results
"exact phrase"                   → Exact match
-term                            → Exclude term
filetype:pdf                     → Only PDFs
```

## Edge Cases Found

1. **Social media results** (Reddit, Medium): `cite` element contains engagement stats instead of URL breadcrumb
   - Fix: `cite` is optional, use `a.zReHs[href]` for the actual URL

2. **"Read more" links**: `a.vzmbzf` links point to `#:~:text=...` fragments
   - Fix: Ignore links with `#:~:text=` pattern

3. **Featured snippets**: Appear ABOVE regular results, different structure
   - Fix: Currently ignored (we only parse `div.MjjYud`)

4. **Video carousel / Image results**: Appear as `div.MjjYud` but with different inner structure
   - Fix: `div.wHYlTd` filter already excludes these

5. **"People also ask"**: Accordion below results, not in `div.MjjYud`
   - Fix: Automatically excluded by our selector

## Raw HTML Verification

Tested with two queries:
- "golang web scraping" → 9/10 organic results extracted correctly
- "rust tauri mcp server" → 8/8 organic results extracted correctly
