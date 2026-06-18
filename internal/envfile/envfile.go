// Package envfile parses .env-style files into the set of keys they declare and
// sanitizes their content. It intentionally ignores values for the drift check —
// only the declared key set matters for "is every used key documented?".
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// File is the parsed contents of a .env-style file.
type File struct {
	// Keys are the declared keys, in first-seen file order.
	Keys   []string
	Values map[string]string
	set    map[string]struct{}
}

// Has reports whether key is declared in the file.
func (f *File) Has(key string) bool {
	_, ok := f.set[key]
	return ok
}

// Parse reads a .env-style file. A missing file is not an error: it returns an
// empty File and exists=false so callers can treat "no .env.example yet" as a
// distinct, reportable state rather than a failure.
func Parse(path string) (file *File, exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newFile(), false, nil
		}
		return nil, false, err
	}
	f := newFile()
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(trimmed[eq+1:])
		if _, ok := f.set[key]; !ok {
			f.Keys = append(f.Keys, key)
		}
		f.set[key] = struct{}{}
		f.Values[key] = val
	}
	return f, true, sc.Err()
}

func newFile() *File {
	return &File{Values: map[string]string{}, set: map[string]struct{}{}}
}

// Sanitize strips stray carriage returns and trailing whitespace from each line
// and reports how many lines it changed. This is the cleanup hosting providers and
// hand-edited .env files most often need before a strict loader will accept them.
func Sanitize(content string) (cleaned string, fixed int) {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		clean := strings.TrimRight(l, " \t\r")
		if clean != l {
			fixed++
		}
		lines[i] = clean
	}
	return strings.Join(lines, "\n"), fixed
}
