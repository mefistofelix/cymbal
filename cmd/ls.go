package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/1broseidon/cymbal/internal/walker"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls [path]",
	Short: "Show file tree, repo list, or repo stats",
	Long: `Show the file tree of a directory (default), list all indexed repos (--repos),
or show repo statistics (--stats).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repos, _ := cmd.Flags().GetBool("repos")
		stats, _ := cmd.Flags().GetBool("stats")
		jsonOut := getJSONFlag(cmd)

		if repos {
			return lsRepos(cmd, jsonOut)
		}
		if stats {
			return lsStats(cmd, jsonOut)
		}
		return lsTree(cmd, args, jsonOut)
	},
}

func init() {
	lsCmd.Flags().Bool("repos", false, "list all indexed repositories")
	lsCmd.Flags().Bool("stats", false, "show repo overview (languages, file/symbol counts)")
	lsCmd.Flags().IntP("depth", "D", 0, "max tree depth (0 = unlimited)")
	rootCmd.AddCommand(lsCmd)
}

func lsRepos(cmd *cobra.Command, jsonOut bool) error {
	dbPath := getDBPath(cmd)
	repos, err := index.ListRepos(dbPath)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No indexed repositories. Run 'cymbal index <path>' first.")
		return nil
	}

	if jsonOut {
		return writeJSON(repos)
	}

	for _, r := range repos {
		fmt.Printf("%-50s  %d files  %d symbols  %s\n",
			r.Path, r.FileCount, r.SymbolCount, r.IndexedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func lsStats(cmd *cobra.Command, jsonOut bool) error {
	dbPath := getDBPath(cmd)
	repo := resolveRepo(cmd)
	if repo == "" {
		return fmt.Errorf("no repo detected — run 'cymbal index <path>' or use --repo")
	}

	stats, err := index.RepoStats(dbPath, repo)
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(stats)
	}

	fmt.Printf("Repository: %s\n", stats.Path)
	fmt.Printf("Files:      %d\n", stats.FileCount)
	fmt.Printf("Symbols:    %d\n", stats.SymbolCount)
	fmt.Printf("Languages:\n")
	for lang, count := range stats.Languages {
		fmt.Printf("  %-16s %d files\n", lang, count)
	}
	return nil
}

func lsTree(cmd *cobra.Command, args []string, jsonOut bool) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")

	tree, err := walker.BuildTree(absPath, maxDepth)
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(tree)
	}

	walker.PrintTree(os.Stdout, tree, "")
	return nil
}
