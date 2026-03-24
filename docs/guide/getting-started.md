# Getting Started

## Install

**Homebrew** (recommended):

```sh
brew install 1broseidon/tap/cymbal
```

**Go** (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/cymbal/releases).

## Quick Start

```sh
# Index the current project (~100ms for most repos)
cymbal index .

# Browse the file tree
cymbal ls

# Find a symbol
cymbal search handleAuth

# Read its source
cymbal show handleAuth

# File outline — all symbols in a file
cymbal outline internal/auth/handler.go

# Who calls this?
cymbal refs handleAuth

# What breaks if I change this?
cymbal refs handleAuth --impact

# Everything you need in one call
cymbal context handleAuth
```

## Agent Integration

cymbal is designed to be an AI agent's code comprehension layer. Add this to your `CLAUDE.md` (or equivalent agent instructions):

```markdown
## Code Exploration Policy
Use `cymbal` CLI for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
- Before reading a file: `cymbal outline <file>` or `cymbal show <file:L1-L2>`
- Before searching: `cymbal search <query>` (symbols) or `cymbal search <query> --text` (grep)
- Before exploring structure: `cymbal ls` (tree) or `cymbal ls --stats` (overview)
- To find usage: `cymbal refs <symbol>` or `cymbal refs <symbol> --importers`
- If a project is not indexed, run `cymbal index .` first (takes <100ms).
- Use `cymbal show <symbol>` to read a specific function/type instead of reading the whole file.
- All commands support `--json` for structured output.
```

This tells the agent to prefer cymbal over grep/find/cat, reducing tool calls and token usage while giving the agent structured, relevant context.

## Supported Languages

cymbal uses tree-sitter grammars. Currently supported:

Go, Python, JavaScript, TypeScript, TSX, Rust, C, C++, C#, Java, Ruby, Swift, Kotlin, Scala, PHP, Lua, Bash, HCL, Dockerfile, YAML, TOML, HTML, CSS

Adding a language requires a tree-sitter grammar and a symbol extraction query — see `internal/parser/` for details.
