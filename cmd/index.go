package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a directory for symbol discovery",
	Long: `Index a directory for symbol discovery. Use --summarize to generate AI summaries
for each symbol using your installed agent CLI (claude, codex, etc.).

No API keys required — cymbal uses oneagent to invoke whatever agent backend
you already have installed and authenticated.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		if _, err := os.Stat(absPath); err != nil {
			return fmt.Errorf("path not found: %s", absPath)
		}

		workers, _ := cmd.Flags().GetInt("workers")
		force, _ := cmd.Flags().GetBool("force")
		summarize, _ := cmd.Flags().GetBool("summarize")
		backend, _ := cmd.Flags().GetString("backend")

		dbPath := getDBPath(cmd)

		fmt.Fprintf(os.Stderr, "Indexing %s ...\n", absPath)
		start := time.Now()

		stats, err := index.Index(absPath, dbPath, index.Options{
			Workers:   workers,
			Force:     force,
			Summarize: summarize,
			Backend:   backend,
		})
		if err != nil {
			return fmt.Errorf("indexing failed: %w", err)
		}

		elapsed := time.Since(start)
		msg := fmt.Sprintf("Done in %s — %d files indexed, %d symbols found, %d skipped (unchanged)",
			elapsed.Round(time.Millisecond), stats.FilesIndexed, stats.SymbolsFound, stats.FilesSkipped)
		if stats.Summarized > 0 {
			msg += fmt.Sprintf(", %d summarized", stats.Summarized)
		}
		fmt.Fprintln(os.Stderr, msg)

		return nil
	},
}

func init() {
	indexCmd.Flags().IntP("workers", "w", 0, "number of parallel workers (0 = NumCPU)")
	indexCmd.Flags().BoolP("force", "f", false, "force re-index all files")
	indexCmd.Flags().Bool("summarize", false, "generate AI summaries using your installed agent CLI")
	indexCmd.Flags().String("backend", "", "agent backend for summaries (default: auto-detect)")
	rootCmd.AddCommand(indexCmd)
}
