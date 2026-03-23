package index

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/1broseidon/cymbal/internal/parser"
	"github.com/1broseidon/cymbal/internal/summarize"
	"github.com/1broseidon/cymbal/internal/symbols"
	"github.com/1broseidon/cymbal/internal/walker"
)

// Options controls indexing behavior.
type Options struct {
	Workers   int
	Force     bool
	Summarize bool
	Backend   string // agent backend for summaries (empty = auto-detect)
}

// Stats reports indexing results.
type Stats struct {
	FilesIndexed int
	FilesSkipped int
	SymbolsFound int
	Summarized   int
	Errors       int
}

// SearchQuery defines a search request.
type SearchQuery struct {
	Text     string
	Kind     string
	Language string
	Exact    bool
	Limit    int
}

// TextResult holds a text search match.
type TextResult struct {
	File    string `json:"file"`
	RelPath string `json:"rel_path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// RepoStatsResult holds repo overview data.
type RepoStatsResult struct {
	Path        string         `json:"path"`
	FileCount   int            `json:"file_count"`
	SymbolCount int            `json:"symbol_count"`
	Languages   map[string]int `json:"languages"`
}

// Index indexes all source files under root.
func Index(root, dbPath string, opts Options) (*Stats, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	defer store.Close()

	repoID, err := store.EnsureRepo(root)
	if err != nil {
		return nil, fmt.Errorf("registering repo: %w", err)
	}

	files, err := walker.Walk(root, workers)
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	var (
		indexed atomic.Int64
		skipped atomic.Int64
		found   atomic.Int64
		errors  atomic.Int64
	)

	ch := make(chan walker.FileEntry, 256)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range ch {
				if !parser.SupportedLanguage(f.Language) {
					skipped.Add(1)
					continue
				}

				if !opts.Force {
					hash, err := HashFile(f.Path)
					if err == nil {
						existingHash, _ := store.FileHash(repoID, f.Path)
						if existingHash == hash {
							skipped.Add(1)
							continue
						}
					}
				}

				result, err := parser.ParseFile(f.Path, f.Language)
				if err != nil {
					errors.Add(1)
					continue
				}

				hash, _ := HashFile(f.Path)
				fileID, err := store.UpsertFile(repoID, f.Path, f.RelPath, f.Language, hash)
				if err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertSymbols(fileID, result.Symbols); err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertImports(fileID, result.Imports); err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertRefs(fileID, result.Refs); err != nil {
					errors.Add(1)
					continue
				}

				indexed.Add(1)
				found.Add(int64(len(result.Symbols)))
			}
		}()
	}

	for _, f := range files {
		ch <- f
	}
	close(ch)
	wg.Wait()

	stats := &Stats{
		FilesIndexed: int(indexed.Load()),
		FilesSkipped: int(skipped.Load()),
		SymbolsFound: int(found.Load()),
		Errors:       int(errors.Load()),
	}

	// Summarization pass — runs after indexing is complete.
	if opts.Summarize && stats.FilesIndexed > 0 {
		summarized, err := runSummarization(store, opts.Backend)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: summarization error: %v\n", err)
		}
		stats.Summarized = summarized
	}

	return stats, nil
}

// runSummarization generates AI summaries for symbols that don't have one yet.
func runSummarization(store *Store, backend string) (int, error) {
	s, err := summarize.New(backend)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(os.Stderr, "Generating summaries via %s ...\n", s.Backend())

	// Find symbols without summaries (only top-level, skip nested/variables).
	rows, err := store.db.Query(`
		SELECT sym.id, sym.name, sym.kind, sym.start_line, sym.end_line, sym.language, f.path
		FROM symbols sym
		JOIN files f ON sym.file_id = f.id
		WHERE (sym.summary IS NULL OR sym.summary = '')
		  AND sym.kind IN ('function', 'method', 'struct', 'class', 'interface', 'trait', 'type', 'enum')
		  AND sym.depth = 0
		ORDER BY f.path, sym.start_line
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type pending struct {
		id        int64
		name      string
		kind      string
		startLine int
		endLine   int
		language  string
		filePath  string
	}

	var items []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name, &p.kind, &p.startLine, &p.endLine, &p.language, &p.filePath); err != nil {
			continue
		}
		items = append(items, p)
	}

	if len(items) == 0 {
		return 0, nil
	}

	count := 0
	for _, item := range items {
		source := readLines(item.filePath, item.startLine, item.endLine)
		if source == "" {
			continue
		}

		sym := symbols.Symbol{
			Name:      item.name,
			Kind:      item.kind,
			StartLine: item.startLine,
			EndLine:   item.endLine,
			Language:  item.language,
			File:      item.filePath,
		}
		summary := s.Summarize(sym, source)
		if summary == "" {
			continue
		}

		_, err := store.db.Exec("UPDATE symbols SET summary = ? WHERE id = ?", summary, item.id)
		if err == nil {
			count++
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s %s\n", count, len(items), item.kind, item.name)
		}
	}

	// Rebuild FTS index after bulk summary updates.
	if count > 0 {
		store.db.Exec("INSERT INTO symbols_fts(symbols_fts) VALUES('rebuild')")
	}

	return count, nil
}

// readLines reads lines startLine..endLine from a file.
func readLines(path string, startLine, endLine int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}
		if lineNum > endLine {
			break
		}
		b.WriteString(scanner.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// ListRepos returns all indexed repos.
func ListRepos(dbPath string) ([]Repo, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListRepos()
}

// FileOutline returns symbols for a file.
func FileOutline(dbPath, filePath string) ([]SymbolResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FileSymbols(filePath)
}

// SearchSymbols searches across all indexed repos.
func SearchSymbols(dbPath string, q SearchQuery) ([]SymbolResult, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.SearchSymbols(q.Text, q.Kind, q.Language, q.Exact, q.Limit)
}

// ResolveRepo finds the nearest indexed repo root by walking up from cwd.
func ResolveRepo(dbPath, cwd string) (string, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	return store.ResolveRepo(cwd)
}

// RepoStats returns overview statistics for a repo.
func RepoStats(dbPath, repoPath string) (*RepoStatsResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.RepoStats(repoPath)
}

// TextSearch greps indexed file contents on disk.
func TextSearch(dbPath, query, lang string, limit int) ([]TextResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	files, err := store.AllFiles(lang)
	if err != nil {
		return nil, err
	}

	queryBytes := []byte(query)
	var mu sync.Mutex
	var results []TextResult
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())

	for _, f := range files {
		if len(results) >= limit {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(path, relPath string) {
			defer wg.Done()
			defer func() { <-sem }()

			file, err := os.Open(path)
			if err != nil {
				return
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Bytes()
				if bytes.Contains(line, queryBytes) {
					mu.Lock()
					if len(results) < limit {
						snippet := string(line)
						if len(snippet) > 200 {
							snippet = snippet[:200]
						}
						results = append(results, TextResult{
							File:    path,
							RelPath: relPath,
							Line:    lineNum,
							Snippet: snippet,
						})
					}
					mu.Unlock()
				}
			}
		}(f.Path, f.RelPath)
	}

	wg.Wait()
	return results, nil
}

// FindReferences finds files that reference a symbol name.
func FindReferences(dbPath, name string, limit int) ([]RefResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindReferences(name, limit)
}

// FindImporters finds files that import the file containing a symbol.
func FindImporters(dbPath, symbolName string, depth, limit int) ([]ImporterResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindImporters(symbolName, depth, limit)
}

// SymbolsByName finds symbols by exact name (for show command).
func SymbolsByName(dbPath, name string) ([]SymbolResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.SearchSymbols(name, "", "", true, 100)
}
