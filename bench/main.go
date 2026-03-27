// bench/main.go — Benchmark harness for cymbal.
//
// Measures: speed, accuracy, token efficiency, JIT freshness, agent workflow savings.
//
// Usage:
//
//	go run ./bench setup   — clone corpus repos into bench/.corpus/
//	go run ./bench run     — execute benchmarks, write bench/RESULTS.md
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Corpus config ──────────────────────────────────────────────────

type Corpus struct {
	Repos []Repo `yaml:"repos"`
}

type Repo struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Ref      string   `yaml:"ref"`
	Language string   `yaml:"language"`
	Symbols  []Symbol `yaml:"symbols"`
}

type Symbol struct {
	Name         string `yaml:"name"`
	FileContains string `yaml:"file_contains"`
	Kind         string `yaml:"kind"`
	ShowContains string `yaml:"show_contains"`
	RefsMin      int    `yaml:"refs_min"`
}

// ── Tool abstraction ───────────────────────────────────────────────

type Op string

const (
	OpIndex       Op = "index"
	OpReindex     Op = "reindex"
	OpSearch      Op = "search"
	OpRefs        Op = "refs"
	OpShow        Op = "show"
	OpInvestigate Op = "investigate"
)

type Tool struct {
	Name    string
	Binary  string
	Ops     map[Op]CmdFunc
	Cleanup func(repoDir string)
}

type CmdFunc func(repoDir, symbol string) *exec.Cmd

// ── Results ────────────────────────────────────────────────────────

type Result struct {
	Tool    string
	Repo    string
	Op      Op
	Symbol  string
	Timings []time.Duration
	Output  int    // bytes of output
	RawOut  string // captured output for accuracy checks
}

func (r Result) Median() time.Duration {
	s := make([]time.Duration, len(r.Timings))
	copy(s, r.Timings)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

type AccuracyCheck struct {
	Repo    string
	Symbol  string
	Op      Op
	Passed  bool
	Details string
}

type FreshnessResult struct {
	Repo     string
	Scenario string
	Latency  time.Duration
	FilesHit int
}

type WorkflowResult struct {
	Repo          string
	Symbol        string
	CymbalCalls   int
	CymbalBytes   int
	BaselineCalls int
	BaselineBytes int
}

// ── Tool definitions ───────────────────────────────────────────────

func cymbalDBPath(repoDir string) string {
	abs, _ := filepath.Abs(repoDir)
	h := sha256.Sum256([]byte(abs))
	home, _ := os.UserCacheDir()
	if home == "" {
		home, _ = os.UserHomeDir()
		if home != "" {
			home = filepath.Join(home, ".cymbal")
		}
	} else {
		home = filepath.Join(home, "cymbal")
	}
	return filepath.Join(home, "repos", hex.EncodeToString(h[:8]), "index.db")
}

func defineTools(cymbalBin string) []Tool {
	return []Tool{
		{
			Name:   "cymbal",
			Binary: cymbalBin,
			Ops: map[Op]CmdFunc{
				OpIndex: func(dir, _ string) *exec.Cmd {
					return exec.Command(cymbalBin, "index", ".")
				},
				OpReindex: func(dir, _ string) *exec.Cmd {
					return exec.Command(cymbalBin, "index", ".")
				},
				OpSearch: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "search", sym)
				},
				OpRefs: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "refs", sym)
				},
				OpShow: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "show", sym)
				},
				OpInvestigate: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "investigate", sym)
				},
			},
			Cleanup: func(dir string) {
				os.Remove(cymbalDBPath(dir))
			},
		},
		{
			Name:   "ripgrep",
			Binary: "rg",
			Ops: map[Op]CmdFunc{
				OpSearch: func(dir, sym string) *exec.Cmd {
					return exec.Command("rg", "--no-heading", "-c", sym)
				},
				OpRefs: func(dir, sym string) *exec.Cmd {
					return exec.Command("rg", "--no-heading", "-n", sym)
				},
				OpShow: func(dir, sym string) *exec.Cmd {
					pattern := "(?:def |func |class |type |interface |struct |async def )" + sym
					return exec.Command("rg", "--no-heading", "-n", "-A", "30", pattern)
				},
			},
		},
	}
}

// ── Core benchmark logic ───────────────────────────────────────────

const (
	indexIters = 3
	queryIters = 5
	warmup     = 1
)

func timeCmd(cmd *exec.Cmd) (time.Duration, []byte, error) {
	start := time.Now()
	out, err := cmd.CombinedOutput()
	return time.Since(start), out, err
}

type preRun func()

func runBench(tool Tool, op Op, repoDir, symbol string, iters int, before ...preRun) Result {
	r := Result{
		Tool:   tool.Name,
		Repo:   filepath.Base(repoDir),
		Op:     op,
		Symbol: symbol,
	}

	for i := 0; i < warmup; i++ {
		for _, fn := range before {
			fn()
		}
		cmd := tool.Ops[op](repoDir, symbol)
		cmd.Dir = repoDir
		cmd.Run()
	}

	for i := 0; i < iters; i++ {
		for _, fn := range before {
			fn()
		}
		cmd := tool.Ops[op](repoDir, symbol)
		cmd.Dir = repoDir
		d, out, err := timeCmd(cmd)
		if err != nil && op != OpSearch {
			fmt.Fprintf(os.Stderr, "  WARN: %s %s %s %s: %v\n", tool.Name, op, r.Repo, symbol, err)
		}
		r.Timings = append(r.Timings, d)
		r.Output = len(out)
		r.RawOut = string(out) // keep last run for accuracy
	}
	return r
}

// ── Accuracy checks ────────────────────────────────────────────────

func checkAccuracy(results []Result, repos []Repo) []AccuracyCheck {
	var checks []AccuracyCheck

	for _, repo := range repos {
		for _, sym := range repo.Symbols {
			// Check search
			if r := findResult2(results, "cymbal", OpSearch, repo.Name, sym.Name); r != nil {
				passed := strings.Contains(r.RawOut, sym.FileContains) &&
					strings.Contains(r.RawOut, sym.Kind)
				detail := ""
				if !passed {
					detail = fmt.Sprintf("expected file=%q kind=%q in output", sym.FileContains, sym.Kind)
				}
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpSearch,
					Passed: passed, Details: detail,
				})
			}

			// Check show
			if r := findResult2(results, "cymbal", OpShow, repo.Name, sym.Name); r != nil {
				passed := strings.Contains(r.RawOut, sym.ShowContains)
				detail := ""
				if !passed {
					detail = fmt.Sprintf("expected %q in show output", sym.ShowContains)
				}
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpShow,
					Passed: passed, Details: detail,
				})
			}

			// Check refs
			if sym.RefsMin > 0 {
				if r := findResult2(results, "cymbal", OpRefs, repo.Name, sym.Name); r != nil {
					// Count non-empty, non-header lines as ref indicators
					lines := strings.Split(strings.TrimSpace(r.RawOut), "\n")
					refLines := 0
					for _, l := range lines {
						if strings.Contains(l, ">") || strings.Contains(l, ":") {
							refLines++
						}
					}
					passed := refLines >= sym.RefsMin
					detail := ""
					if !passed {
						detail = fmt.Sprintf("expected >=%d refs, got %d indicator lines", sym.RefsMin, refLines)
					}
					checks = append(checks, AccuracyCheck{
						Repo: repo.Name, Symbol: sym.Name, Op: OpRefs,
						Passed: passed, Details: detail,
					})
				}
			}

			// Check investigate
			if r := findResult2(results, "cymbal", OpInvestigate, repo.Name, sym.Name); r != nil {
				passed := strings.Contains(r.RawOut, sym.ShowContains)
				detail := ""
				if !passed {
					detail = fmt.Sprintf("expected %q in investigate output", sym.ShowContains)
				}
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpInvestigate,
					Passed: passed, Details: detail,
				})
			}
		}
	}
	return checks
}

// ── JIT Freshness benchmark ────────────────────────────────────────

func benchFreshness(cymbalBin string, repos []Repo, corpusDir string) []FreshnessResult {
	var results []FreshnessResult

	for _, repo := range repos {
		dir := filepath.Join(corpusDir, repo.Name)
		sym := repo.Symbols[0].Name

		// Ensure indexed first
		cmd := exec.Command(cymbalBin, "index", ".", "--force")
		cmd.Dir = dir
		cmd.Run()

		// Scenario 1: hot (nothing changed)
		for i := 0; i < warmup; i++ {
			c := exec.Command(cymbalBin, "search", sym)
			c.Dir = dir
			c.Run()
		}
		d := medianOf(func() time.Duration {
			c := exec.Command(cymbalBin, "search", sym)
			c.Dir = dir
			start := time.Now()
			c.Run()
			return time.Since(start)
		}, 5)
		results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "hot (no changes)", Latency: d, FilesHit: 0})

		// Scenario 2: touch 1 file
		files := findSourceFiles(dir, 5)
		if len(files) >= 1 {
			touch(files[0])
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "1 file touched", Latency: d, FilesHit: 1})
		}

		// Scenario 3: touch 5 files
		if len(files) >= 5 {
			for _, f := range files[:5] {
				touch(f)
			}
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "5 files touched", Latency: d, FilesHit: 5})
		}

		// Scenario 4: delete + query (prune)
		if len(files) >= 1 {
			// Create a temp file, index it, then delete
			tmpFile := filepath.Join(dir, "_bench_tmp_delete_test.go")
			os.WriteFile(tmpFile, []byte("package main\nfunc BenchDeleteTest() {}\n"), 0644)
			c := exec.Command(cymbalBin, "index", ".")
			c.Dir = dir
			c.Run()
			os.Remove(tmpFile)
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "1 file deleted (prune)", Latency: d, FilesHit: 1})
		}
	}
	return results
}

func findSourceFiles(dir string, n int) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".py", ".js", ".ts", ".rs", ".java", ".rb":
			files = append(files, path)
		}
		return nil
	})
	if len(files) > n {
		return files[:n]
	}
	return files
}

func touch(path string) {
	now := time.Now()
	os.Chtimes(path, now, now)
}

func medianOf(fn func() time.Duration, n int) time.Duration {
	var times []time.Duration
	for i := 0; i < n; i++ {
		times = append(times, fn())
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return times[len(times)/2]
}

func singleTimed(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}

// ── Agent workflow comparison ──────────────────────────────────────

func benchWorkflow(cymbalBin string, repos []Repo, corpusDir string) []WorkflowResult {
	var results []WorkflowResult

	for _, repo := range repos {
		dir := filepath.Join(corpusDir, repo.Name)

		for _, sym := range repo.Symbols {
			// cymbal: 1 call = investigate
			cmd := exec.Command(cymbalBin, "investigate", sym.Name)
			cmd.Dir = dir
			cymOut, _ := cmd.CombinedOutput()

			// baseline: 3 calls = rg search + rg show + rg refs
			var baselineBytes int
			baselineCalls := 0

			cmd = exec.Command("rg", "--no-heading", "-c", sym.Name)
			cmd.Dir = dir
			out, _ := cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			pattern := "(?:def |func |class |type |interface |struct |async def )" + sym.Name
			cmd = exec.Command("rg", "--no-heading", "-n", "-A", "30", pattern)
			cmd.Dir = dir
			out, _ = cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			cmd = exec.Command("rg", "--no-heading", "-n", sym.Name)
			cmd.Dir = dir
			out, _ = cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			results = append(results, WorkflowResult{
				Repo:          repo.Name,
				Symbol:        sym.Name,
				CymbalCalls:   1,
				CymbalBytes:   len(cymOut),
				BaselineCalls: baselineCalls,
				BaselineBytes: baselineBytes,
			})
		}
	}
	return results
}

// ── Setup command ──────────────────────────────────────────────────

func cmdSetup(corpus Corpus, corpusDir string) error {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return err
	}

	for _, repo := range corpus.Repos {
		dest := filepath.Join(corpusDir, repo.Name)
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("  %s: already cloned\n", repo.Name)
			continue
		}
		fmt.Printf("  %s: cloning %s @ %s ...\n", repo.Name, repo.URL, repo.Ref)
		cmd := exec.Command("git", "clone", "--depth=1", "--branch", repo.Ref, repo.URL, dest)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cloning %s: %w", repo.Name, err)
		}
	}

	fmt.Println("\nCorpus ready.")
	return nil
}

// ── Run command ────────────────────────────────────────────────────

func cmdRun(corpus Corpus, corpusDir, cymbalBin string) error {
	tools := defineTools(cymbalBin)

	var available []Tool
	for _, t := range tools {
		if _, err := exec.LookPath(t.Binary); err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP: %s not found (%s)\n", t.Name, t.Binary)
			continue
		}
		available = append(available, t)
	}
	if len(available) == 0 {
		return fmt.Errorf("no tools available")
	}

	var results []Result

	// Phase 1: Speed + Token Efficiency
	fmt.Println("\n=== Phase 1: Speed + Token Efficiency ===")
	for _, repo := range corpus.Repos {
		dir := filepath.Join(corpusDir, repo.Name)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("corpus repo %s not found — run: go run ./bench setup", repo.Name)
		}

		fmt.Printf("\n== %s (%s) ==\n", repo.Name, repo.Language)

		for _, tool := range available {
			fmt.Printf("  %s:\n", tool.Name)

			if _, ok := tool.Ops[OpIndex]; ok {
				var before []preRun
				if tool.Cleanup != nil {
					before = append(before, func() { tool.Cleanup(dir) })
				}
				fmt.Printf("    index (cold) ...")
				r := runBench(tool, OpIndex, dir, "", indexIters, before...)
				fmt.Printf(" %v\n", r.Median())
				results = append(results, r)
			}

			if _, ok := tool.Ops[OpReindex]; ok {
				fmt.Printf("    reindex ...")
				r := runBench(tool, OpReindex, dir, "", indexIters)
				fmt.Printf(" %v\n", r.Median())
				results = append(results, r)
			}

			symNames := make([]string, len(repo.Symbols))
			for i, s := range repo.Symbols {
				symNames[i] = s.Name
			}

			for _, sym := range symNames {
				for _, op := range []Op{OpSearch, OpRefs, OpShow, OpInvestigate} {
					if _, ok := tool.Ops[op]; !ok {
						continue
					}
					fmt.Printf("    %s(%s) ...", op, sym)
					r := runBench(tool, op, dir, sym, queryIters)
					fmt.Printf(" %v\n", r.Median())
					results = append(results, r)
				}
			}
		}
	}

	// Phase 2: Accuracy
	fmt.Println("\n=== Phase 2: Accuracy ===")
	accuracy := checkAccuracy(results, corpus.Repos)
	passed, total := 0, len(accuracy)
	for _, a := range accuracy {
		if a.Passed {
			passed++
			fmt.Printf("  ✓ %s/%s/%s\n", a.Repo, a.Symbol, a.Op)
		} else {
			fmt.Printf("  ✗ %s/%s/%s: %s\n", a.Repo, a.Symbol, a.Op, a.Details)
		}
	}
	fmt.Printf("\n  Accuracy: %d/%d (%.0f%%)\n", passed, total, float64(passed)/float64(total)*100)

	// Phase 3: JIT Freshness
	fmt.Println("\n=== Phase 3: JIT Freshness ===")
	freshness := benchFreshness(cymbalBin, corpus.Repos, corpusDir)
	for _, f := range freshness {
		fmt.Printf("  %s | %-25s | %v\n", f.Repo, f.Scenario, f.Latency.Round(time.Millisecond))
	}

	// Phase 4: Agent Workflow
	fmt.Println("\n=== Phase 4: Agent Workflow ===")
	workflows := benchWorkflow(cymbalBin, corpus.Repos, corpusDir)
	for _, w := range workflows {
		savings := 0
		if w.BaselineBytes > 0 {
			savings = 100 - (w.CymbalBytes*100)/w.BaselineBytes
		}
		fmt.Printf("  %s/%s: cymbal=%d calls/%dB, baseline=%d calls/%dB, savings=%d%%\n",
			w.Repo, w.Symbol, w.CymbalCalls, w.CymbalBytes, w.BaselineCalls, w.BaselineBytes, savings)
	}

	// Generate report
	report := generateReport(results, available, accuracy, freshness, workflows)
	outPath := filepath.Join("bench", "RESULTS.md")
	if err := os.WriteFile(outPath, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nResults written to %s\n", outPath)
	return nil
}

// ── Report generation ──────────────────────────────────────────────

func generateReport(results []Result, tools []Tool, accuracy []AccuracyCheck, freshness []FreshnessResult, workflows []WorkflowResult) string {
	var b strings.Builder

	b.WriteString("# Cymbal Benchmark Results\n\n")
	b.WriteString(fmt.Sprintf("**Date:** %s  \n", time.Now().Format("2006-01-02 15:04 MST")))
	b.WriteString(fmt.Sprintf("**Platform:** %s/%s  \n", runtime.GOOS, runtime.GOARCH))
	b.WriteString(fmt.Sprintf("**CPU:** %d cores  \n\n", runtime.NumCPU()))

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name
	}

	// Group results by repo
	byRepo := map[string][]Result{}
	for _, r := range results {
		byRepo[r.Repo] = append(byRepo[r.Repo], r)
	}
	repos := sortedKeys(byRepo)

	// ── Per-repo speed + token tables ──
	for _, repo := range repos {
		b.WriteString(fmt.Sprintf("## %s\n\n", repo))

		// Indexing
		b.WriteString("### Indexing\n\n")
		b.WriteString("| Operation |")
		for _, tn := range toolNames {
			b.WriteString(fmt.Sprintf(" %s |", tn))
		}
		b.WriteString("\n|---|")
		for range toolNames {
			b.WriteString("---|")
		}
		b.WriteString("\n")

		for _, op := range []Op{OpIndex, OpReindex} {
			b.WriteString(fmt.Sprintf("| %s |", op))
			for _, tn := range toolNames {
				r := findResult2(results, tn, op, repo, "")
				if r == nil {
					b.WriteString(" — |")
				} else {
					b.WriteString(fmt.Sprintf(" %s |", fmtDuration(r.Median())))
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")

		// Query speed
		b.WriteString("### Query Speed\n\n")
		b.WriteString("| Symbol | Op |")
		for _, tn := range toolNames {
			b.WriteString(fmt.Sprintf(" %s |", tn))
		}
		b.WriteString("\n|---|---|")
		for range toolNames {
			b.WriteString("---|")
		}
		b.WriteString("\n")

		pairs := collectPairs(byRepo[repo])
		for _, p := range pairs {
			b.WriteString(fmt.Sprintf("| `%s` | %s |", p.sym, p.op))
			for _, tn := range toolNames {
				r := findResult2(results, tn, Op(p.op), repo, p.sym)
				if r == nil {
					b.WriteString(" — |")
				} else {
					b.WriteString(fmt.Sprintf(" %s |", fmtDuration(r.Median())))
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")

		// Token efficiency with savings ratio
		b.WriteString("### Token Efficiency\n\n")
		b.WriteString("| Symbol | Op | cymbal | ripgrep | savings |\n")
		b.WriteString("|---|---|---|---|---|\n")

		for _, p := range pairs {
			cymR := findResult2(results, "cymbal", Op(p.op), repo, p.sym)
			rgR := findResult2(results, "ripgrep", Op(p.op), repo, p.sym)

			cymTok, rgTok, savingsStr := "—", "—", "—"
			if cymR != nil {
				cymTok = fmt.Sprintf("%s (~%d tok)", fmtBytes(cymR.Output), cymR.Output/4)
			}
			if rgR != nil {
				rgTok = fmt.Sprintf("%s (~%d tok)", fmtBytes(rgR.Output), rgR.Output/4)
			}
			if cymR != nil && rgR != nil && rgR.Output > 0 {
				savings := 100 - (cymR.Output*100)/rgR.Output
				if savings < 0 {
					savingsStr = fmt.Sprintf("-%d%%", -savings)
				} else {
					savingsStr = fmt.Sprintf("**%d%%**", savings)
				}
			}
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n", p.sym, p.op, cymTok, rgTok, savingsStr))
		}
		b.WriteString("\n")
	}

	// ── Accuracy ──
	b.WriteString("## Accuracy\n\n")
	b.WriteString("| Repo | Symbol | Op | Result |\n")
	b.WriteString("|---|---|---|---|\n")
	passed, total := 0, len(accuracy)
	for _, a := range accuracy {
		mark := "✓"
		if !a.Passed {
			mark = "✗ " + a.Details
		} else {
			passed++
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n", a.Repo, a.Symbol, a.Op, mark))
	}
	b.WriteString(fmt.Sprintf("\n**Overall: %d/%d (%.0f%%)**\n\n", passed, total, float64(passed)/float64(total)*100))

	// ── JIT Freshness ──
	b.WriteString("## JIT Freshness\n\n")
	b.WriteString("| Repo | Scenario | Latency |\n")
	b.WriteString("|---|---|---|\n")
	for _, f := range freshness {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", f.Repo, f.Scenario, fmtDuration(f.Latency)))
	}
	b.WriteString("\n")

	// ── Agent Workflow ──
	b.WriteString("## Agent Workflow: cymbal investigate vs ripgrep\n\n")
	b.WriteString("| Repo | Symbol | cymbal | baseline (rg×3) | savings |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, w := range workflows {
		savings := 0
		if w.BaselineBytes > 0 {
			savings = 100 - (w.CymbalBytes*100)/w.BaselineBytes
		}
		savingsStr := fmt.Sprintf("%d%%", savings)
		if savings > 0 {
			savingsStr = fmt.Sprintf("**%d%%**", savings)
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | %d call, %s (~%d tok) | %d calls, %s (~%d tok) | %s |\n",
			w.Repo, w.Symbol,
			w.CymbalCalls, fmtBytes(w.CymbalBytes), w.CymbalBytes/4,
			w.BaselineCalls, fmtBytes(w.BaselineBytes), w.BaselineBytes/4,
			savingsStr))
	}
	b.WriteString("\n")

	return b.String()
}

// ── Helpers ────────────────────────────────────────────────────────

func findResult2(results []Result, tool string, op Op, repo, symbol string) *Result {
	for i := range results {
		r := &results[i]
		if r.Tool == tool && r.Op == op && r.Repo == repo && r.Symbol == symbol {
			return r
		}
	}
	return nil
}

type symOp struct{ sym, op string }

func collectPairs(results []Result) []symOp {
	seen := map[symOp]bool{}
	var pairs []symOp
	for _, r := range results {
		if r.Op == OpIndex || r.Op == OpReindex {
			continue
		}
		so := symOp{r.Symbol, string(r.Op)}
		if !seen[so] {
			seen[so] = true
			pairs = append(pairs, so)
		}
	}
	return pairs
}

func sortedKeys(m map[string][]Result) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fmtDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func fmtBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}

// ── Entrypoint ─────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: go run ./bench [setup|run]\n")
		os.Exit(1)
	}

	corpusFile := filepath.Join("bench", "corpus.yaml")
	data, err := os.ReadFile(corpusFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", corpusFile, err)
		os.Exit(1)
	}

	var corpus Corpus
	if err := yaml.Unmarshal(data, &corpus); err != nil {
		fmt.Fprintf(os.Stderr, "parsing %s: %v\n", corpusFile, err)
		os.Exit(1)
	}

	corpusDir := filepath.Join("bench", ".corpus")

	cymbalBin := "cymbal"
	if bin, err := exec.LookPath("./cymbal"); err == nil {
		cymbalBin, _ = filepath.Abs(bin)
	} else if bin, err := exec.LookPath("cymbal"); err == nil {
		cymbalBin = bin
	}

	switch os.Args[1] {
	case "setup":
		fmt.Println("Setting up benchmark corpus...")
		if err := cmdSetup(corpus, corpusDir); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
	case "run":
		fmt.Println("Running benchmarks...")
		fmt.Printf("Using cymbal: %s\n", cymbalBin)
		if err := cmdRun(corpus, corpusDir, cymbalBin); err != nil {
			fmt.Fprintf(os.Stderr, "run: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: go run ./bench [setup|run]\n", os.Args[1])
		os.Exit(1)
	}
}
