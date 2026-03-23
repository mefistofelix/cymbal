// Package summarize generates AI summaries for code symbols using oneagent.
// No API keys required — uses whatever agent CLIs (claude, codex, etc.) the user
// already has installed and authenticated.
package summarize

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/1broseidon/oneagent"
	"github.com/1broseidon/cymbal/internal/symbols"
)

// Summarizer generates one-line descriptions for code symbols.
type Summarizer struct {
	backends map[string]oneagent.Backend
	backend  string
	mu       sync.Mutex
}

// New creates a Summarizer. Backend is optional — defaults to "claude".
func New(backend string) (*Summarizer, error) {
	backends, err := oneagent.LoadBackends("")
	if err != nil {
		return nil, fmt.Errorf("loading backends: %w", err)
	}

	if backend == "" {
		backend = pickAvailableBackend(backends)
	}

	if _, ok := backends[backend]; !ok {
		return nil, fmt.Errorf("backend %q not found — available: %s", backend, availableBackends(backends))
	}

	return &Summarizer{
		backends: backends,
		backend:  backend,
	}, nil
}

// Summarize generates a one-line summary for a symbol given its source code.
func (s *Summarizer) Summarize(sym symbols.Symbol, source string) string {
	prompt := fmt.Sprintf(
		"Summarize this %s %s in exactly one sentence (max 15 words). No preamble, no quotes, just the summary:\n\n```%s\n%s\n```",
		sym.Language, sym.Kind, sym.Language, source,
	)

	resp := oneagent.Run(s.backends, oneagent.RunOpts{
		Backend: s.backend,
		Prompt:  prompt,
	})

	if resp.Error != "" {
		return ""
	}

	result := strings.TrimSpace(resp.Result)
	// Strip quotes if the model wrapped it.
	result = strings.Trim(result, "\"'`")
	// Cap at one line.
	if idx := strings.IndexByte(result, '\n'); idx >= 0 {
		result = result[:idx]
	}
	return result
}

// Backend returns the backend being used.
func (s *Summarizer) Backend() string {
	return s.backend
}

// pickAvailableBackend tries common backends in preference order.
func pickAvailableBackend(backends map[string]oneagent.Backend) string {
	for _, name := range []string{"claude", "codex", "opencode", "pi", "gemini"} {
		if b, ok := backends[name]; ok {
			if _, found := oneagent.ResolveBackendProgram(b); found {
				return name
			}
		}
	}
	// Fall back to first available.
	for name, b := range backends {
		if _, found := oneagent.ResolveBackendProgram(b); found {
			return name
		}
	}
	return "claude"
}

func availableBackends(backends map[string]oneagent.Backend) string {
	var names []string
	for name, b := range backends {
		if _, found := oneagent.ResolveBackendProgram(b); found {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "(none installed)"
	}
	return strings.Join(names, ", ")
}

// Available reports whether at least one backend is usable.
func Available() bool {
	backends, err := oneagent.LoadBackends("")
	if err != nil {
		return false
	}
	for _, b := range backends {
		if _, found := oneagent.ResolveBackendProgram(b); found {
			return true
		}
	}
	return false
}

// PrintAvailable prints which backends are ready to use.
func PrintAvailable() {
	backends, err := oneagent.LoadBackends("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No backends available: %v\n", err)
		return
	}
	for name, b := range backends {
		_, found := oneagent.ResolveBackendProgram(b)
		status := "not found"
		if found {
			status = "ready"
		}
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", name, status)
	}
}
