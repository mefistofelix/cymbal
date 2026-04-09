package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var outlineCmd = &cobra.Command{
	Use:   "outline <file>",
	Short: "Show symbols defined in a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		sigs, _ := cmd.Flags().GetBool("signatures")

		symbols, err := index.FileOutline(dbPath, filePath)
		if err != nil {
			return err
		}

		if len(symbols) == 0 {
			fmt.Fprintf(os.Stderr, "No symbols found. Is the file indexed? Run 'cymbal index %s'\n",
				filepath.Dir(filePath))
			return nil
		}

		if jsonOut {
			return writeJSON(symbols)
		}

		relPath := args[0]

		var content strings.Builder
		for _, s := range symbols {
			indent := strings.Repeat("  ", s.Depth)
			line := fmt.Sprintf("%s%s %s", indent, s.Kind, s.Name)
			if sigs && s.Signature != "" {
				line += s.Signature
			}
			line += fmt.Sprintf(" (L%d-%d)", s.StartLine, s.EndLine)
			content.WriteString(line)
			content.WriteByte('\n')
		}

		frontmatter([]kv{
			{"file", relPath},
			{"symbol_count", fmt.Sprintf("%d", len(symbols))},
		}, content.String())
		return nil
	},
}

func init() {
	outlineCmd.Flags().BoolP("signatures", "s", false, "show full parameter signatures")
	rootCmd.AddCommand(outlineCmd)
}
