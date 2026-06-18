// Package drift compares the keys a codebase uses against the keys an .env example
// declares, yielding the two failure modes that bite in CI and production:
// used-but-undeclared ("missing") and declared-but-unused ("stale").
package drift

import "sort"

// Diff returns keys referenced in code but absent from the example (missing) and
// keys declared in the example but never referenced (stale). Both are sorted.
//
// missing is the dangerous set — a deploy reads a variable nobody documented, so
// staging/production crash on a value that was never provisioned. stale is hygiene:
// dead documentation that drifts further from the code every release.
func Diff(used, declared []string) (missing, stale []string) {
	u := toSet(used)
	d := toSet(declared)

	for k := range u {
		if _, ok := d[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range d {
		if _, ok := u[k]; !ok {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return missing, stale
}

func toSet(keys []string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}
