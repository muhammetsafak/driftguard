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

// Result is the set of env-var keys referenced in a scanned tree, each mapped to
// where it was first seen.
type Result struct {
	Keys map[string]Location
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
	res  []*regexp.Regexp
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
	},
	{
		exts: exts(".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"),
		res: mustAll(
			`process\.env\.([A-Za-z_][A-Za-z0-9_]*)`,
			`process\.env\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`Deno\.env\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
		),
	},
	{
		exts: exts(".php"),
		res: mustAll(
			`getenv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
			`\$_(?:ENV|SERVER)\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`\benv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
		),
	},
	{
		exts: exts(".py"),
		res: mustAll(
			`os\.environ\.get\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
			`os\.environ\[\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]\s*\]`,
			`os\.getenv\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)['"]`,
		),
	},
}

func patternsFor(ext string) []*regexp.Regexp {
	for _, g := range groups {
		if _, ok := g.exts[ext]; ok {
			return g.res
		}
	}
	return nil
}

// Dir scans root recursively and returns every statically-referenced env key.
func Dir(root string) (Result, error) {
	res := Result{Keys: map[string]Location{}}
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
		pats := patternsFor(strings.ToLower(filepath.Ext(path)))
		if len(pats) == 0 {
			return nil
		}
		return scanFile(path, pats, res.Keys)
	})
	return res, err
}

func scanFile(path string, pats []*regexp.Regexp, into map[string]Location) error {
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
		for _, re := range pats {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				key := m[1]
				if _, ok := into[key]; !ok {
					into[key] = Location{File: path, Line: line}
				}
			}
		}
	}
	return sc.Err()
}
