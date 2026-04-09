package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var refsCmd = &cobra.Command{
	Use:   "refs <symbol> [symbol2 ...]",
	Short: "Find references to a symbol (best-effort)",
	Long: `Find files and lines that reference a symbol name.

Default: shows call-expression references across indexed files.
--importers: shows files that import the file defining this symbol.
--impact: shorthand for --importers --depth 2 (transitive impact).

Supports batch: cymbal refs Foo Bar Baz

Note: references are best-effort based on AST name matching, not semantic analysis.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		importers, _ := cmd.Flags().GetBool("importers")
		impact, _ := cmd.Flags().GetBool("impact")
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		ctx, _ := cmd.Flags().GetInt("context")

		if impact {
			importers = true
			if depth < 2 {
				depth = 2
			}
		}

		for i, name := range args {
			if i > 0 {
				fmt.Println()
			}
			var err error
			if importers {
				err = refsImporters(dbPath, name, depth, limit, jsonOut)
			} else {
				err = refsSymbol(dbPath, name, limit, ctx, jsonOut)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			}
		}
		return nil
	},
}

func init() {
	refsCmd.Flags().Bool("importers", false, "find files that import the defining file")
	refsCmd.Flags().Bool("impact", false, "transitive impact analysis (--importers --depth 2)")
	refsCmd.Flags().IntP("depth", "D", 1, "import chain depth for --importers (max 3)")
	refsCmd.Flags().IntP("limit", "n", 20, "max results")
	refsCmd.Flags().IntP("context", "C", 1, "lines of context around each call site (0 for single-line)")
	rootCmd.AddCommand(refsCmd)
}

func refsSymbol(dbPath, name string, limit, ctx int, jsonOut bool) error {
	results, err := index.FindReferences(dbPath, name, limit)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "No references found for '%s'.\n", name)
		return nil
	}

	if jsonOut {
		return writeJSON(enrichRefs(results, ctx))
	}

	var refs []refLine
	for _, r := range results {
		ctxLines, ctxStart := readSourceContext(r.File, r.Line, ctx)
		refs = append(refs, refLine{
			relPath:      r.RelPath,
			line:         r.Line,
			text:         strings.TrimSpace(readSourceLine(r.File, r.Line)),
			contextLines: ctxLines,
			contextStart: ctxStart,
		})
	}
	lines, groups := dedupRefLines(refs)

	var content strings.Builder
	for _, l := range lines {
		content.WriteString(l)
		content.WriteByte('\n')
	}

	meta := []kv{{"symbol", name}}
	if groups < len(results) {
		meta = append(meta, kv{"groups", fmt.Sprintf("%d", groups)})
		meta = append(meta, kv{"total_refs", fmt.Sprintf("%d", len(results))})
	} else {
		meta = append(meta, kv{"ref_count", fmt.Sprintf("%d", len(results))})
	}
	frontmatter(meta, content.String())
	return nil
}

func refsImporters(dbPath, name string, depth, limit int, jsonOut bool) error {
	results, err := index.FindImporters(dbPath, name, depth, limit)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "No importers found for '%s'.\n", name)
		return nil
	}

	if jsonOut {
		return writeJSON(results)
	}

	var content strings.Builder
	for _, r := range results {
		fmt.Fprintf(&content, "%s:%s\n", r.RelPath, r.Import)
	}

	frontmatter([]kv{
		{"symbol", name},
		{"importer_count", fmt.Sprintf("%d", len(results))},
	}, content.String())
	return nil
}
