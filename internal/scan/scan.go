// Package scan finds the environment-variable keys a codebase actually reads,
// by statically matching the canonical env-access idioms of each language. It is
// deliberately conservative: it extracts only literal, statically-knowable keys
// (e.g. os.Getenv("X"), process.env.X) and never executes anything.
package scan

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Location is where a key was first seen.
type Location struct {
	File string
	Line int
}

// DynamicRef is a spot where the code reads env with a non-literal key (e.g.
// getenv($name), $_ENV[$k], a PHP variable-variable). The key can't be known
// statically, so it is reported as a blind spot for a human to review rather than
// recorded as a drift key — DriftGuard never guesses a name it can't see.
type DynamicRef struct {
	Location
	Snippet string // the trimmed source line, for context in the report
}

// Result is the set of env-var keys referenced in a scanned tree, each mapped to
// where it was first seen, plus the dynamic (non-literal) accesses it could not
// resolve to a concrete key.
type Result struct {
	Keys    map[string]Location
	Dynamic map[Location]string // location → trimmed source line
}

// SortedKeys returns the referenced keys in lexical order.
func (r Result) SortedKeys() []string {
	out := make([]string, 0, len(r.Keys))
	for k := range r.Keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SortedDynamic returns the dynamic accesses in file, then line order.
func (r Result) SortedDynamic() []DynamicRef {
	out := make([]DynamicRef, 0, len(r.Dynamic))
	for loc, snippet := range r.Dynamic {
		out = append(out, DynamicRef{Location: loc, Snippet: snippet})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// ignoredDirs are skipped wholesale during the walk — generated output, vendored
// dependencies, and editor/VCS metadata never define a project's own env contract.
var ignoredDirs = map[string]struct{}{
	"node_modules": {}, "vendor": {}, ".git": {}, "dist": {}, "build": {},
	".next": {}, ".nuxt": {}, ".svelte-kit": {}, ".venv": {}, "venv": {},
	"__pycache__": {}, ".idea": {}, ".vscode": {}, "coverage": {}, ".cache": {},
	"tmp": {}, ".turbo": {},
}

type group struct {
	exts map[string]struct{}
	res  []*regexp.Regexp // literal-key patterns; capture group 1 is the key

	// dyn matches a line that reads env with a non-literal key — recorded as a
	// blind spot (DynamicRef), never as a key. dynAll is for keys that only count
	// in env context: each inner slice is a conjunction (the line must match ALL of
	// them), used so a bare PHP variable-variable is flagged only when the same line
	// also touches an env token — an unrelated `$$foo` stays quiet.
	dyn    []*regexp.Regexp
	dynAll [][]*regexp.Regexp
}

// matchesDynamic reports whether line contains a non-literal env access.
func (g *group) matchesDynamic(line string) bool {
	for _, re := range g.dyn {
		if re.MatchString(line) {
			return true
		}
	}
	for _, conj := range g.dynAll {
		if len(conj) == 0 {
			continue
		}
		all := true
		for _, re := range conj {
			if !re.MatchString(line) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func mustAll(pats ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(pats))
	for i, p := range pats {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

func exts(list ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, e := range list {
		m[e] = struct{}{}
	}
	return m
}

// key = `[A-Za-z_][A-Za-z0-9_]*`, captured in group 1.
var groups = []group{
	{
		exts: exts(".go"),
		res: mustAll(
			`os\.(?:Getenv|LookupEnv)\(\s*"([A-Za-z_][A-Za-z0-9_]*)"`,
		),
		dyn: mustAll(
			// A non-literal arg: first non-space char is not a string delimiter.
			// Go's backtick (\x60) raw string is static, so it is excluded too.
			`os\.(?:Getenv|LookupEnv)\(\s*[^"\x60)\s]`, // os.Getenv(name), os.Getenv(pfx+"X")
		),
	},
	{
		exts: exts(".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"),
		res: mustAll(
			`process\.env\.([A-Za-z_][A-Za-z0-9_]*)`,
			`process\.env\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`Deno\.env\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
		),
		dyn: mustAll(
			// Backtick (template literal) is treated as dynamic — it is usually
			// interpolated (`${x}_URL`), so it stays outside the excluded set.
			`process\.env\[\s*[^'"\]\s]`,  // process.env[name], process.env[`${x}`]
			`Deno\.env\.get\(\s*[^'")\s]`, // Deno.env.get(name)
		),
	},
	{
		exts: exts(".php"),
		res: mustAll(
			`getenv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
			`\$_(?:ENV|SERVER)\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`\benv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
			`\barray_key_exists\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*,\s*\$_(?:ENV|SERVER)\s*\)`,
		),
		dyn: mustAll(
			`getenv\(\s*\$`,            // getenv($var)
			`\benv\(\s*\$`,             // env($var)  — Laravel helper
			`\$_(?:ENV|SERVER)\[\s*\$`, // $_ENV[$var] / $_SERVER[$var]
		),
		dynAll: [][]*regexp.Regexp{
			// A PHP variable-variable ($$name) resolves its key at runtime, so it is
			// flagged only when the same line also references env — otherwise `$$x` is
			// ordinary variable-variable use, unrelated to env drift.
			mustAll(`\$\$[A-Za-z_]`, `\$_(?:ENV|SERVER)\b|\bgetenv\s*\(|\benv\s*\(`),
		},
	},
	{
		exts: exts(".py"),
		res: mustAll(
			`os\.environ\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
			`os\.environ\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`os\.getenv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
		),
		dyn: mustAll(
			// A leading f (f-string) or identifier is dynamic; a quoted literal is not.
			`os\.environ\[\s*[^'"\]\s]`,     // os.environ[name], os.environ[f'{x}']
			`os\.environ\.get\(\s*[^'")\s]`, // os.environ.get(name)
			`os\.getenv\(\s*[^'")\s]`,       // os.getenv(name), os.getenv(f"PRE_{x}")
		),
	},
}

func groupFor(ext string) *group {
	for i := range groups {
		if _, ok := groups[i].exts[ext]; ok {
			return &groups[i]
		}
	}
	return nil
}

// Dir scans root recursively and returns every statically-referenced env key
// plus every non-literal (dynamic) env access it could not resolve.
func Dir(root string) (Result, error) {
	res := Result{Keys: map[string]Location{}, Dynamic: map[Location]string{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := ignoredDirs[d.Name()]; skip && path != root {
				return fs.SkipDir
			}
			return nil
		}
		g := groupFor(strings.ToLower(filepath.Ext(path)))
		if g == nil {
			return nil
		}
		return scanFile(path, g, &res)
	})
	return res, err
}

func scanFile(path string, g *group, res *Result) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		for _, re := range g.res {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				key := m[1]
				if _, ok := res.Keys[key]; !ok {
					res.Keys[key] = Location{File: path, Line: line}
				}
			}
		}
		if g.matchesDynamic(text) {
			loc := Location{File: path, Line: line}
			if _, ok := res.Dynamic[loc]; !ok {
				res.Dynamic[loc] = strings.TrimSpace(text)
			}
		}
	}
	return sc.Err()
}
