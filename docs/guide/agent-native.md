# Agent-Native Output

cymbal's default output format is designed for AI agents — not humans reading a terminal.

## Frontmatter + Content

Every command returns YAML frontmatter (structured metadata) followed by a content body (source code, results, etc.). This format is optimized for LLM token efficiency: agents can parse the metadata programmatically and read the content naturally.

```yaml
---
symbol: handleAuth
kind: function
file: internal/auth/handler.go
lines: 42-87
language: go
---

func handleAuth(w http.ResponseWriter, r *http.Request) {
    token := r.Header.Get("Authorization")
    ...
}
```

## Why Not JSON?

JSON is verbose. Every field name is quoted, every string is escaped, and nested structures add layers of braces. For an LLM that processes tokens, this overhead adds up fast.

Compare the same `refs` output:

**JSON** (~180 tokens):
```json
{
  "symbol": "handleAuth",
  "total": 3,
  "results": [
    {"file": "cmd/server/main.go", "line": 23, "text": "handleAuth(w, r)"},
    {"file": "internal/api/router.go", "line": 45, "text": "mux.HandleFunc(\"/auth\", handleAuth)"},
    {"file": "internal/api/router.go", "line": 67, "text": "handleAuth(w, r)"}
  ]
}
```

**Frontmatter + Content** (~120 tokens):
```yaml
---
symbol: handleAuth
total: 3
groups: 2
---

cmd/server/main.go (1 site):
  > handleAuth(w, r)

internal/api/router.go (2 sites):
  > mux.HandleFunc("/auth", handleAuth)
  > handleAuth(w, r)
```

The frontmatter format is ~33% fewer tokens with the same information. At scale — across dozens of tool calls per task — this compounds.

## Smart Deduplication

When multiple call sites in the same file look identical, cymbal groups them. The agent sees the pattern ("this function is called 5 times in router.go") without wasting tokens on repetitive lines.

All commands still support `--json` for structured output when you need machine-parseable results.
