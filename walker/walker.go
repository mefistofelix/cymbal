package walker

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// FileEntry is a file discovered during walking.
type FileEntry struct {
	Path     string
	RelPath  string
	Size     int64
	Language string
	ModTime  time.Time
}

// TreeNode represents a directory/file tree.
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Children []*TreeNode `json:"children,omitempty"`
}

// Known directories to skip.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".tox":         true,
	".mypy_cache":  true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".nuxt":        true,
	"target":       true, // Rust/Java
	".idea":        true,
	".vscode":      true,
}

type gitignoreRule struct {
	baseDir  string
	pattern  string
	negated  bool
	dirOnly  bool
	anchored bool
	basename bool
}

// Language extension mapping.
var extToLang = map[string]string{
	".go":      "go",
	".py":      "python",
	".js":      "javascript",
	".jsx":     "javascript",
	".ts":      "typescript",
	".tsx":     "typescript",
	".rs":      "rust",
	".rb":      "ruby",
	".java":    "java",
	".c":       "c",
	".h":       "c",
	".cpp":     "cpp",
	".cc":      "cpp",
	".hpp":     "cpp",
	".cls":     "apex",
	".trigger": "apex",
	".cs":      "csharp",
	".swift":   "swift",
	".kt":      "kotlin",
	".lua":     "lua",
	".php":     "php",
	".sh":      "bash",
	".bash":    "bash",
	".zsh":     "bash",
	".zig":     "zig",
	".toml":    "toml",
	".yaml":    "yaml",
	".yml":     "yaml",
	".json":    "json",
	".md":      "markdown",
	".sql":     "sql",
	".proto":   "protobuf",
	".tf":      "hcl",
	".hcl":     "hcl",
	".ex":      "elixir",
	".exs":     "elixir",
	".erl":     "erlang",
	".hs":      "haskell",
	".ml":      "ocaml",
	".mli":     "ocaml",
	".scala":   "scala",
	".r":       "r",
	".R":       "r",
	".pl":      "perl",
	".pm":      "perl",
	".dart":    "dart",
	".vue":     "vue",
	".svelte":  "svelte",
}

// LangForFile returns the language identifier for a file path.
func LangForFile(path string) string {
	ext := filepath.Ext(path)
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	// Check special filenames
	base := filepath.Base(path)
	switch base {
	case "Makefile", "makefile", "GNUmakefile":
		return "make"
	case "Dockerfile":
		return "dockerfile"
	case "Jenkinsfile":
		return "groovy"
	case "CMakeLists.txt":
		return "cmake"
	}
	return ""
}

// Walk concurrently discovers all source files under root.
// If langFilter is non-nil, only files whose language passes the filter are emitted.
// This avoids building FileEntry structs and stat-ing files that will be immediately
// skipped (e.g., .json, .md, .toml that the parser doesn't support).
func Walk(root string, workers int, langFilter func(string) bool) ([]FileEntry, error) {
	if workers <= 0 {
		workers = 8
	}

	var mu sync.Mutex
	var files []FileEntry

	ch := make(chan string, 256)
	var wg sync.WaitGroup

	// Spawn workers that process directories.
	for range workers {
		wg.Go(func() {
			for path := range ch {
				lang := LangForFile(path)
				if lang == "" {
					continue
				}
				if langFilter != nil && !langFilter(lang) {
					continue
				}
				info, err := os.Lstat(path)
				if err != nil {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				entry := FileEntry{
					Path:     path,
					RelPath:  rel,
					Size:     info.Size(),
					Language: lang,
					ModTime:  info.ModTime(),
				}
				mu.Lock()
				files = append(files, entry)
				mu.Unlock()
			}
		})
	}

	err := walkFiles(root, root, nil, ch)
	close(ch)
	wg.Wait()

	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
}

// BuildTree constructs a tree representation of the directory.
func BuildTree(root string, maxDepth int) (*TreeNode, error) {
	rootNode := &TreeNode{
		Name:  filepath.Base(root),
		Path:  root,
		IsDir: true,
	}

	err := buildTreeRecursive(rootNode, root, root, nil, 1, maxDepth)
	if err != nil {
		return nil, err
	}
	return rootNode, nil
}

func walkFiles(root, dirPath string, parentRules []gitignoreRule, ch chan<- string) error {
	rules := rulesForDir(root, dirPath, parentRules)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(dirPath, name)
		relPath, err := filepath.Rel(root, fullPath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			if shouldSkipDir(name, relPath, rules) {
				continue
			}
			if err := walkFiles(root, fullPath, rules, ch); err != nil {
				return err
			}
			continue
		}

		if matchesGitignore(rules, relPath, false) {
			continue
		}
		ch <- fullPath
	}
	return nil
}

func buildTreeRecursive(node *TreeNode, root, dirPath string, parentRules []gitignoreRule, depth, maxDepth int) error {
	if maxDepth > 0 && depth > maxDepth {
		return nil
	}

	rules := rulesForDir(root, dirPath, parentRules)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		name := e.Name()
		childPath := filepath.Join(dirPath, name)
		relPath, err := filepath.Rel(root, childPath)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(relPath)

		child := &TreeNode{
			Name:  name,
			Path:  childPath,
			IsDir: e.IsDir(),
		}

		if e.IsDir() {
			if shouldSkipDir(name, relPath, rules) {
				continue
			}
			buildTreeRecursive(child, root, child.Path, rules, depth+1, maxDepth)
		} else if matchesGitignore(rules, relPath, false) {
			continue
		}

		node.Children = append(node.Children, child)
	}
	return nil
}

func shouldSkipDir(name, relPath string, rules []gitignoreRule) bool {
	return skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") || matchesGitignore(rules, relPath, true)
}

func rulesForDir(root, dirPath string, parentRules []gitignoreRule) []gitignoreRule {
	rules := append([]gitignoreRule(nil), parentRules...)
	localRules, err := loadGitignoreRules(root, dirPath)
	if err == nil && len(localRules) > 0 {
		rules = append(rules, localRules...)
	}
	return rules
}

func loadGitignoreRules(root, dirPath string) ([]gitignoreRule, error) {
	data, err := os.ReadFile(filepath.Join(dirPath, ".gitignore"))
	if err != nil {
		return nil, err
	}

	baseDir, err := filepath.Rel(root, dirPath)
	if err != nil {
		return nil, err
	}
	baseDir = filepath.ToSlash(baseDir)
	if baseDir == "." {
		baseDir = ""
	}

	var rules []gitignoreRule
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule := gitignoreRule{baseDir: baseDir}
		if strings.HasPrefix(line, "!") {
			rule.negated = true
			line = line[1:]
		}

		line = filepath.ToSlash(line)
		if strings.HasPrefix(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}

		rule.pattern = line
		rule.basename = !strings.Contains(line, "/")
		rules = append(rules, rule)
	}

	return rules, nil
}

func matchesGitignore(rules []gitignoreRule, relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	ignored := false
	for _, rule := range rules {
		if rule.matches(relPath, isDir) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func (rule gitignoreRule) matches(relPath string, isDir bool) bool {
	if rule.dirOnly && !isDir {
		return false
	}
	if rule.baseDir != "" {
		prefix := rule.baseDir + "/"
		if relPath == rule.baseDir {
			return false
		}
		if !strings.HasPrefix(relPath, prefix) {
			return false
		}
		relPath = strings.TrimPrefix(relPath, prefix)
	}
	if relPath == "" {
		return false
	}

	if rule.basename {
		matched, _ := doublestar.Match(rule.pattern, path.Base(relPath))
		return matched
	}

	if matchGitignorePath(rule.pattern, relPath) {
		return true
	}
	if rule.anchored {
		return false
	}

	for i := 0; i < len(relPath); i++ {
		if relPath[i] == '/' && matchGitignorePath(rule.pattern, relPath[i+1:]) {
			return true
		}
	}
	return false
}

func matchGitignorePath(pattern, name string) bool {
	matched, err := doublestar.Match(pattern, name)
	return err == nil && matched
}

// PrintTree writes an ASCII tree to the writer.
func PrintTree(w io.Writer, node *TreeNode, prefix string) {
	if node == nil {
		return
	}

	io.WriteString(w, node.Name+"\n")

	for i, child := range node.Children {
		isLast := i == len(node.Children)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "└── "
			childPrefix = "    "
		}

		io.WriteString(w, prefix+connector)
		PrintTree(w, child, prefix+childPrefix)
	}
}
