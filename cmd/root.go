package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cymbal",
	Short: "Fast code indexer and symbol discovery tool",
	Long: `Cymbal is a blazing-fast code indexer, parser, and symbol discovery CLI.
It uses tree-sitter for multi-language AST parsing and SQLite for indexed storage,
designed to be called by AI agents and developer tools.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("db", "d", defaultDBPath(), "path to cymbal database")
	rootCmd.PersistentFlags().Bool("json", false, "output as JSON")
	rootCmd.PersistentFlags().String("repo", "", "explicit repo root (default: auto-detect from CWD)")
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cymbal.db"
	}
	return fmt.Sprintf("%s/.cymbal/cymbal.db", home)
}

func getDBPath(cmd *cobra.Command) string {
	p, _ := cmd.Flags().GetString("db")
	return p
}

func getJSONFlag(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// resolveRepo auto-detects the repo root from CWD or uses --repo flag.
func resolveRepo(cmd *cobra.Command) string {
	if r, _ := cmd.Flags().GetString("repo"); r != "" {
		abs, err := filepath.Abs(r)
		if err != nil {
			return r
		}
		return abs
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dbPath := getDBPath(cmd)
	repo, _ := index.ResolveRepo(dbPath, cwd)
	return repo
}
