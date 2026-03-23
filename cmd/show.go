package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/1broseidon/cymbal/internal/walker"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <symbol|file[:L1-L2]>",
	Short: "Read source by symbol name or file path",
	Long: `Show source code for a symbol or file.

If the argument contains '/' or ends with a known extension, it's treated as a file path.
Otherwise, it's treated as a symbol name.

Examples:
  cymbal show ParseFile              # show symbol source
  cymbal show internal/index/store.go     # show full file
  cymbal show internal/index/store.go:80-120  # show lines 80-120`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		dbPath := getDBPath(cmd)
		jsonOut := getJSONFlag(cmd)
		ctx, _ := cmd.Flags().GetInt("context")

		if isFilePath(target) {
			return showFile(target, ctx, jsonOut)
		}
		return showSymbol(dbPath, target, ctx, jsonOut)
	},
}

func init() {
	showCmd.Flags().IntP("context", "C", 0, "lines of context around the target")
	rootCmd.AddCommand(showCmd)
}

// isFilePath returns true if the target looks like a file path.
func isFilePath(target string) bool {
	// Contains path separator.
	if strings.Contains(target, "/") {
		return true
	}
	// Strip :L1-L2 suffix before checking extension.
	clean := target
	if idx := strings.LastIndex(clean, ":"); idx > 0 {
		clean = clean[:idx]
	}
	return walker.LangForFile(clean) != ""
}

// parseFileTarget parses "file.go:100-150" into path, start, end.
func parseFileTarget(target string) (string, int, int) {
	idx := strings.LastIndex(target, ":")
	if idx <= 0 {
		return target, 0, 0
	}

	path := target[:idx]
	rangeStr := target[idx+1:]

	parts := strings.SplitN(rangeStr, "-", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return target, 0, 0
	}

	end := start
	if len(parts) == 2 {
		if e, err := strconv.Atoi(parts[1]); err == nil {
			end = e
		}
	}
	return path, start, end
}

func showFile(target string, ctx int, jsonOut bool) error {
	path, startLine, endLine := parseFileTarget(target)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("file not found: %s", path)
	}
	defer f.Close()

	// Apply context.
	if startLine > 0 && ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
	}

	type lineEntry struct {
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	var lines []lineEntry

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if startLine > 0 && lineNum < startLine {
			continue
		}
		if endLine > 0 && lineNum > endLine {
			break
		}
		lines = append(lines, lineEntry{Line: lineNum, Content: scanner.Text()})
	}

	if jsonOut {
		return writeJSON(map[string]any{
			"file":  absPath,
			"lines": lines,
		})
	}

	for _, l := range lines {
		fmt.Printf("%4d  %s\n", l.Line, l.Content)
	}
	return nil
}

func showSymbol(dbPath, name string, ctx int, jsonOut bool) error {
	results, err := index.SymbolsByName(dbPath, name)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "Symbol not found: %s\n", name)
		os.Exit(1)
	}

	// Multiple matches — disambiguate.
	if len(results) > 1 {
		if jsonOut {
			return writeJSON(map[string]any{
				"ambiguous": true,
				"matches":   results,
			})
		}
		fmt.Fprintf(os.Stderr, "Multiple matches for '%s' — be more specific:\n", name)
		for _, r := range results {
			fmt.Printf("  %-12s %-40s %s:%d-%d\n", r.Kind, r.Name, r.RelPath, r.StartLine, r.EndLine)
		}
		os.Exit(1)
	}

	sym := results[0]
	startLine := sym.StartLine
	endLine := sym.EndLine
	if ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
	}

	target := fmt.Sprintf("%s:%d-%d", sym.File, startLine, endLine)
	return showFile(target, 0, jsonOut)
}
