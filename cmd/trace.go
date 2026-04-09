package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var traceCmd = &cobra.Command{
	Use:   "trace <symbol>",
	Short: "Downward call trace — what does this symbol call?",
	Long: `Follow the call graph downward from a symbol: what it calls,
what those call, etc. Complementary to impact (which traces upward).

  investigate = "tell me about X"
  trace       = "what does X depend on?"
  impact      = "what depends on X?"

Examples:
  cymbal trace handleRegister           # 3-deep call chain
  cymbal trace handleRegister -d 5      # deeper trace
  cymbal trace handleRegister -n 20     # limit results`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")

		fileHint, symName := parseSymbolArg(name)
		_ = fileHint // trace resolves internally

		results, err := index.FindTrace(dbPath, symName, depth, limit)
		if err != nil {
			return err
		}

		if jsonOut {
			return writeJSON(results)
		}

		if len(results) == 0 {
			fmt.Printf("No outgoing calls found for '%s'.\n", symName)
			return nil
		}

		var content strings.Builder
		for _, tr := range results {
			fmt.Fprintf(&content, "  [%d] %s → %s  %s:%d\n",
				tr.Depth, tr.Caller, tr.Callee, tr.RelPath, tr.Line)
		}

		frontmatter([]kv{
			{"symbol", symName},
			{"direction", "downward (callees)"},
			{"depth", fmt.Sprintf("%d", depth)},
			{"edges", fmt.Sprintf("%d", len(results))},
		}, content.String())
		return nil
	},
}

func init() {
	traceCmd.Flags().Int("depth", 3, "max traversal depth")
	traceCmd.Flags().IntP("limit", "n", 50, "max results")
	rootCmd.AddCommand(traceCmd)
}
