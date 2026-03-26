# cymbal

Fast, language-agnostic code indexer and symbol navigator built on [tree-sitter](https://tree-sitter.github.io/).

cymbal parses your codebase into a local SQLite index, then gives you instant symbol search, cross-references, impact analysis, and scoped diffs — all from the command line. Designed to be called by AI agents, editor plugins, or directly from your terminal.

## Install

Homebrew:

```sh
brew install 1broseidon/tap/cymbal
```

Or with Go (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/cymbal/releases).

## Quick start

```sh
# Index the current project
cymbal index .

# Investigate any symbol — one call, right answer
cymbal investigate handleAuth    # function → source + callers + impact
cymbal investigate UserModel     # type → definition + members + references
cymbal trace handleAuth          # downward call chain — what does it call?

# Or use specific commands when you need control
cymbal search handleAuth         # find a symbol
cymbal search "TODO" --text      # full-text grep
cymbal show handleAuth           # read source
cymbal outline internal/auth/handler.go  # file structure
cymbal refs handleAuth           # who calls this?
cymbal importers internal/auth   # who imports this package?
cymbal impact handleAuth         # what breaks if I change this?
cymbal diff handleAuth main      # git diff scoped to a function
cymbal context handleAuth        # bundled: source + types + callers + imports
cymbal ls                        # file tree
```

## Commands

| Command | What it does |
|---------|-------------|
| `investigate` | **Start here.** Kind-adaptive exploration — one call, right shape |
| `trace` | Downward call graph — what does this symbol call? |
| `index` | Parse and index a directory |
| `ls` | File tree, repo list, or `--stats` overview |
| `search` | Symbol search (or `--text` for grep) |
| `show` | Display a symbol's source code |
| `outline` | List all symbols in a file |
| `refs` | Find references / call sites |
| `importers` | Reverse import lookup — who imports this? |
| `impact` | Transitive callers — what's affected by a change? |
| `diff` | Git diff scoped to a symbol's line range |
| `context` | Bundled view: source + types + callers + imports |

All commands support `--json` for structured output.

## Agent integration

cymbal is designed as the code navigation layer for AI agents. One command handles most investigations — specific commands exist as escape hatches when you need more control.

Add this to your agent's system prompt (e.g., `CLAUDE.md`, `agent.md`, or MCP tool descriptions):

```markdown
## Code Exploration Policy
Use `cymbal` CLI for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
- **To understand a symbol**: `cymbal investigate <symbol>` — returns source, callers, impact, or members based on what the symbol is. Use this first.
- **To trace an execution path**: `cymbal trace <symbol>` — follows the call graph downward (what does X call, what do those call).
- **To assess change risk**: `cymbal impact <symbol>` — follows the call graph upward (what breaks if X changes).
- Before reading a file: `cymbal outline <file>` or `cymbal show <file:L1-L2>`
- Before searching: `cymbal search <query>` (symbols) or `cymbal search <query> --text` (grep)
- Before exploring structure: `cymbal ls` (tree) or `cymbal ls --stats` (overview)
- To disambiguate: `cymbal show path/to/file.go:SymbolName` or `cymbal investigate file.go:Symbol`
- If a project is not indexed, run `cymbal index .` first (takes <100ms).
- All commands support `--json` for structured output.
```

### Why this works

An agent tracing an auth flow typically makes 15-20 sequential tool calls: show function → read the code → guess the next function → show that → repeat. Each call costs a reasoning step (~500 tokens). Three commands eliminate this:

| Command | Question it answers | Direction |
|---|---|---|
| `investigate X` | "Tell me about X" | Adaptive (source + callers + impact or members) |
| `trace X` | "What does X depend on?" | Downward (callees, depth 3) |
| `impact X` | "What depends on X?" | Upward (callers, depth 2) |

`investigate` replaces search → show → refs. `trace` replaces 10+ sequential show calls to follow a call chain. Together they reduce a 22-call investigation to 4 calls.

## Supported languages

cymbal uses tree-sitter grammars. Currently supported:

Go, Python, JavaScript, TypeScript, TSX, Rust, C, C++, C#, Java, Ruby, Swift, Kotlin, Scala, PHP, Lua, Bash, HCL, Dockerfile, YAML, TOML, HTML, CSS

Adding a language requires a tree-sitter grammar and a symbol extraction query — see `internal/parser/` for examples.

## How it works

1. **Index** — tree-sitter parses each file into an AST. cymbal extracts symbols (functions, types, variables, imports) and references (calls, type usage) and stores them in SQLite with FTS5 full-text search. Each repo gets its own database at `~/.cymbal/repos/<hash>/index.db`.

2. **Query** — all commands read from the current repo's SQLite index. Symbol lookups, cross-references, and import graphs are SQL queries. No re-parsing needed. No cross-repo bleed.

3. **Incremental** — re-indexing skips unchanged files using mtime (nanosecond) + file size checks. Only changed files are re-parsed and re-hashed. Reindex completes in 2-15ms for most repos.

## Docs

- [Changelog](./CHANGELOG.md)

## License

[MIT](./LICENSE)
