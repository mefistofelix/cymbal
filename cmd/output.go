package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/internal/index"
)

// writeJSON writes a versioned JSON envelope to stdout.
func writeJSON(data any) error {
	envelope := map[string]any{
		"version": "0.1",
		"results": data,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

// frontmatter writes YAML-style frontmatter followed by content.
// Keys are printed in the order provided.
func frontmatter(meta []kv, content string) {
	fmt.Println("---")
	for _, m := range meta {
		fmt.Printf("%s: %s\n", m.k, m.v)
	}
	fmt.Println("---")
	if content != "" {
		fmt.Print(content)
		// Ensure trailing newline.
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
	}
}

// kv is an ordered key-value pair for frontmatter output.
type kv struct {
	k, v string
}

// refLine is a single reference with file, line, source text, and surrounding context.
type refLine struct {
	relPath      string
	line         int
	text         string
	contextLines []string // lines around the call site (len = 2*ctx+1 when available)
	contextStart int      // line number of the first context line
}

// dedupRefLines groups identical source text per file.
// Returns formatted lines ready to print and the number of unique groups.
func dedupRefLines(refs []refLine) ([]string, int) {
	type key struct{ path, text string }
	type group struct {
		path         string
		text         string
		lines        []int
		contextLines []string // from first occurrence
		contextStart int
		callLine     int // line number of the call (first occurrence)
	}

	seen := make(map[key]*group)
	var order []key

	for _, r := range refs {
		k := key{r.relPath, r.text}
		if g, ok := seen[k]; ok {
			g.lines = append(g.lines, r.line)
		} else {
			seen[k] = &group{
				path:         r.relPath,
				text:         r.text,
				lines:        []int{r.line},
				contextLines: r.contextLines,
				contextStart: r.contextStart,
				callLine:     r.line,
			}
			order = append(order, k)
		}
	}

	var out []string
	for _, k := range order {
		g := seen[k]
		hasContext := len(g.contextLines) > 1 // more than just the call line itself

		// Header line
		var header string
		if len(g.lines) == 1 {
			header = fmt.Sprintf("%s:%d:", g.path, g.lines[0])
		} else {
			header = fmt.Sprintf("%s (%d sites):", g.path, len(g.lines))
		}

		if !hasContext {
			// No context — single-line format (same as --context 0)
			out = append(out, fmt.Sprintf("%s %s", header, g.text))
		} else {
			out = append(out, header)
			for i, cl := range g.contextLines {
				lineNo := g.contextStart + i
				if lineNo == g.callLine {
					out = append(out, fmt.Sprintf("  > %s", cl))
				} else {
					out = append(out, fmt.Sprintf("    %s", cl))
				}
			}
		}
	}
	return out, len(order)
}

// readSourceLine reads a single line from a file on disk.
// Returns the trimmed-right content or "" on error.
func readSourceLine(path string, lineNum int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	cur := 0
	for scanner.Scan() {
		cur++
		if cur == lineNum {
			return scanner.Text()
		}
	}
	return ""
}

// readSourceContext reads lines [lineNum-ctx, lineNum+ctx] from a file.
// Returns the lines (trimmed right) and the 1-based start line number.
// Handles edge cases at file boundaries gracefully.
func readSourceContext(path string, lineNum, ctx int) ([]string, int) {
	if ctx <= 0 {
		text := readSourceLine(path, lineNum)
		return []string{strings.TrimRight(text, " \t")}, lineNum
	}

	startLine := max(lineNum-ctx, 1)
	endLine := lineNum + ctx

	f, err := os.Open(path)
	if err != nil {
		return []string{""}, lineNum
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	cur := 0
	for scanner.Scan() {
		cur++
		if cur > endLine {
			break
		}
		if cur >= startLine {
			lines = append(lines, strings.TrimRight(scanner.Text(), " \t"))
		}
	}
	if len(lines) == 0 {
		return []string{""}, lineNum
	}
	return lines, startLine
}

// enrichedRef wraps a RefResult with surrounding context lines for JSON output.
type enrichedRef struct {
	index.RefResult
	Context []string `json:"context,omitempty"`
}

// enrichRefs adds source context lines to each ref result.
func enrichRefs(results []index.RefResult, ctx int) []enrichedRef {
	out := make([]enrichedRef, len(results))
	for i, r := range results {
		ctxLines, _ := readSourceContext(r.File, r.Line, ctx)
		out[i] = enrichedRef{RefResult: r, Context: ctxLines}
	}
	return out
}

// enrichedImpact wraps an ImpactResult with surrounding context lines for JSON output.
type enrichedImpact struct {
	index.ImpactResult
	Context []string `json:"context,omitempty"`
}

// enrichImpact adds source context lines to each impact result.
func enrichImpact(results []index.ImpactResult, ctx int) []enrichedImpact {
	out := make([]enrichedImpact, len(results))
	for i, r := range results {
		ctxLines, _ := readSourceContext(r.File, r.Line, ctx)
		out[i] = enrichedImpact{ImpactResult: r, Context: ctxLines}
	}
	return out
}
