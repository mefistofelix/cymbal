package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var investigateCmd = &cobra.Command{
	Use:   "investigate <symbol>",
	Short: "Kind-adaptive investigation — returns the right context for what a symbol is",
	Long: `Investigate a symbol and get back the right shape of information
based on what it is. No need to choose between search, show, refs,
or impact — cymbal looks at the symbol's kind and returns what matters.

  function/method → source + callers + shallow impact
  class/struct/type/interface → source + members + references
  ambiguous → ranked candidates with file and kind

Examples:
  cymbal investigate OpenStore
  cymbal investigate SymbolResult
  cymbal investigate ParseFile`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		dbPath := getDBPath(cmd)
		jsonOut := getJSONFlag(cmd)

		fileHint, symName := parseSymbolArg(name)
		var opts []index.InvestigateOpts
		if fileHint != "" {
			opts = append(opts, index.InvestigateOpts{FileHint: fileHint})
		}
		result, err := index.Investigate(dbPath, symName, opts...)
		if err != nil {
			var ambig *index.AmbiguousError
			if errors.As(err, &ambig) {
				if jsonOut {
					return writeJSON(map[string]any{
						"ambiguous": true,
						"matches":   ambig.Matches,
					})
				}
				fmt.Fprintf(os.Stderr, "Multiple matches for '%s' — be more specific:\n", name)
				for _, r := range ambig.Matches {
					fmt.Printf("  %-12s %-40s %s:%d-%d\n", r.Kind, r.Name, r.RelPath, r.StartLine, r.EndLine)
				}
				os.Exit(1)
			}
			return err
		}

		if jsonOut {
			return writeJSON(result)
		}

		sym := result.Symbol
		var content strings.Builder

		// Source section.
		content.WriteString("# Source\n")
		src := strings.TrimRight(result.Source, "\n")
		content.WriteString(src)
		content.WriteByte('\n')

		// Members section (types).
		if len(result.Members) > 0 {
			fmt.Fprintf(&content, "\n# Members (%d)\n", len(result.Members))
			for _, m := range result.Members {
				fmt.Fprintf(&content, "  %-12s %s", m.Kind, m.Name)
				if m.Signature != "" {
					fmt.Fprintf(&content, " %s", m.Signature)
				}
				fmt.Fprintf(&content, "  %s:%d\n", m.RelPath, m.StartLine)
			}
		}

		// Refs section.
		if len(result.Refs) > 0 {
			var refs []refLine
			for _, r := range result.Refs {
				refs = append(refs, refLine{
					relPath: r.RelPath,
					line:    r.Line,
					text:    strings.TrimSpace(readSourceLine(r.File, r.Line)),
				})
			}
			lines, _ := dedupRefLines(refs)
			label := "References"
			if result.Kind == "function" {
				label = "Callers"
			}
			fmt.Fprintf(&content, "\n# %s (%d)\n", label, len(lines))
			for _, l := range lines {
				content.WriteString(l)
				content.WriteByte('\n')
			}
		}

		// Impact section (functions only).
		if len(result.Impact) > 0 {
			fmt.Fprintf(&content, "\n# Impact (depth 2)\n")
			for _, imp := range result.Impact {
				fmt.Fprintf(&content, "  [%d] %s → %s  %s:%d\n",
					imp.Depth, imp.Caller, imp.Symbol, imp.RelPath, imp.Line)
			}
		}

		frontmatter([]kv{
			{"symbol", sym.Name},
			{"kind", sym.Kind},
			{"investigate", result.Kind},
			{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
		}, content.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(investigateCmd)
}
