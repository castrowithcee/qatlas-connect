// Command sizecheck enforces size limits on the module's Go source: lines per file, lines per function and
// cyclomatic complexity per function. Run it from the module root. Generated files and testdata directories
// are exempt, and test files have a higher file limit.
//
// Violations that existed when the limits were introduced are listed in exceptions.txt, each with its
// measured value as ceiling. The list can only shrink: every entry must appear unchanged in baseline.txt, the
// list as measured at introduction, whose hash is pinned below, and an entry that no longer exceeds a limit
// fails the check until it is removed.
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	maxFileLines     = 500
	maxTestFileLines = 1000
	maxFuncLines     = 60
	maxComplexity    = 15
)

// baselineHash pins baseline.txt with carriage returns removed. Changing it adds exceptions, which the limits
// do not allow.
const baselineHash = "8d3ea68967856eb8a21fa8e83b4b06bd30f91cd0ba8b474728cc2759fa0a6322"

var (
	//go:embed baseline.txt
	baseline string
	//go:embed exceptions.txt
	exceptions string
)

func main() {
	list := flag.Bool("list", false, "print every current violation in the list format instead of checking")
	flag.Parse()
	if _, err := os.Stat("go.mod"); err != nil {
		fail(fmt.Errorf("run from the module root: %w", err))
	}
	found, err := measure(os.DirFS("."))
	if err != nil {
		fail(err)
	}
	if *list {
		fmt.Print(format(found))
		return
	}
	problems := verifyBaseline(baseline)
	problems = append(problems, check(found, exceptions, baseline)...)
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "sizecheck: %v\n", err)
	os.Exit(2)
}

// size is a measured or allowed size. Files have no complexity.
type size struct {
	lines, complexity int
}

func isFunc(key string) bool { return strings.Contains(key, ":") }

// limit is the size a key may reach without an exception.
func limit(key string) size {
	switch {
	case isFunc(key):
		return size{maxFuncLines, maxComplexity}
	case strings.HasSuffix(key, "_test.go"):
		return size{maxTestFileLines, 0}
	default:
		return size{maxFileLines, 0}
	}
}

func (s size) exceeds(max size) bool { return s.lines > max.lines || s.complexity > max.complexity }

// measure returns every file and function in fsys that exceeds its limit, keyed by slash path and, for a
// function, path:name.
func measure(fsys fs.FS) (map[string]size, error) {
	found := map[string]size{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		return measureFile(fsys, p, found)
	})
	return found, err
}

// skipDir follows the go tool, which ignores these directories in package patterns.
func skipDir(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func measureFile(fsys fs.FS, p string, found map[string]size) error {
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	if ast.IsGenerated(f) {
		return nil
	}
	if s := (size{lineCount(src), 0}); s.exceeds(limit(p)) {
		found[p] = s
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key := p + ":" + funcName(fn)
		s := size{fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1, complexity(fn)}
		if !s.exceeds(limit(key)) {
			continue
		}
		// Several init functions in one file share a key; the largest counts.
		prev := found[key]
		found[key] = size{max(prev.lines, s.lines), max(prev.complexity, s.complexity)}
	}
	return nil
}

func lineCount(src []byte) int {
	n := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}

// funcName is Name for a function and (T).Name or (*T).Name for a method, without type parameters.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t, ptr := fn.Recv.List[0].Type, ""
	if star, ok := t.(*ast.StarExpr); ok {
		t, ptr = star.X, "*"
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return "(" + ptr + id.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}

// complexity counts like gocyclo: one plus each if, for, range, non-default case and select case, && and ||.
// Function literals count toward the enclosing function.
func complexity(fn *ast.FuncDecl) int {
	c := 1
	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			c++
		case *ast.CaseClause:
			if n.List != nil {
				c++
			}
		case *ast.CommClause:
			if n.Comm != nil {
				c++
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				c++
			}
		}
		return true
	})
	return c
}

// format writes one entry per line, sorted by key: "path lines" for a file, "path:name lines complexity" for a
// function.
func format(found map[string]size) string {
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(entryLine(k, found[k]) + "\n")
	}
	return b.String()
}

func entryLine(key string, s size) string {
	if isFunc(key) {
		return fmt.Sprintf("%s %d %d", key, s.lines, s.complexity)
	}
	return fmt.Sprintf("%s %d", key, s.lines)
}

func verifyBaseline(text string) []string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(text, "\r", "")))
	if hex.EncodeToString(sum[:]) != baselineHash {
		return []string{"baseline.txt: changed; it holds the violations measured when the limits were introduced and never changes"}
	}
	return nil
}

// check compares the measured violations with the exceptions and returns one line per problem.
func check(found map[string]size, exceptionText, baselineText string) []string {
	allowed, problems := parseExceptions(exceptionText, baselineText)
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if p := judge(k, found[k], allowed); p != "" {
			problems = append(problems, p)
		}
	}
	stale := []string{}
	for k := range allowed {
		if _, ok := found[k]; !ok {
			stale = append(stale, "exceptions.txt: "+k+" no longer exceeds a limit; remove the entry")
		}
	}
	sort.Strings(stale)
	return append(problems, stale...)
}

// judge reports a violation without exception or beyond its exception. An exception raises only the limits
// its entry exceeded.
func judge(key string, s size, allowed map[string]size) string {
	lim := limit(key)
	ceiling, ok := allowed[key]
	if ok {
		ceiling = size{max(lim.lines, ceiling.lines), max(lim.complexity, ceiling.complexity)}
	} else {
		ceiling = lim
	}
	if !s.exceeds(ceiling) {
		return ""
	}
	var parts []string
	if s.lines > ceiling.lines {
		parts = append(parts, fmt.Sprintf("%d lines, allowed %d", s.lines, ceiling.lines))
	}
	if s.complexity > ceiling.complexity {
		parts = append(parts, fmt.Sprintf("complexity %d, allowed %d", s.complexity, ceiling.complexity))
	}
	return key + ": " + strings.Join(parts, ", ") + "; split it up, the exceptions do not grow"
}

// parseExceptions reads the entries that also appear unchanged in the baseline.
func parseExceptions(exceptionText, baselineText string) (map[string]size, []string) {
	inBaseline := map[string]bool{}
	for _, line := range entries(baselineText) {
		inBaseline[line] = true
	}
	allowed := map[string]size{}
	var problems []string
	for _, line := range entries(exceptionText) {
		key, s, err := parseEntry(line)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("exceptions.txt: %q: %v", line, err))
		case !inBaseline[line]:
			problems = append(problems, fmt.Sprintf("exceptions.txt: %q is not in baseline.txt; the list only shrinks", line))
		case allowed[key] != size{}:
			problems = append(problems, fmt.Sprintf("exceptions.txt: %s is listed twice", key))
		default:
			allowed[key] = s
		}
	}
	return allowed, problems
}

// entries returns the normalized entry lines, without blank lines and # comments.
func entries(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func parseEntry(line string) (string, size, error) {
	f := strings.Fields(line)
	want := 2
	if isFunc(f[0]) {
		want = 3
	}
	if len(f) != want {
		return "", size{}, fmt.Errorf("want \"path lines\" or \"path:name lines complexity\"")
	}
	var s size
	var err error
	if s.lines, err = strconv.Atoi(f[1]); err == nil && want == 3 {
		s.complexity, err = strconv.Atoi(f[2])
	}
	return f[0], s, err
}
