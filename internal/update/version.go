// Package update reports the running version and replaces the binary with
// the latest GitHub release.
package update

import (
	"runtime/debug"
	"strconv"
	"strings"
)

// Resolve picks the version to report: the release ldflag, else the module
// version recorded by `go install module@vX`, else "dev".
func Resolve(ldflag string) string {
	if ldflag != "" {
		return strings.TrimPrefix(ldflag, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" && !strings.Contains(v, "-0.") {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}

// Newer reports whether version a is newer than b (both "X.Y.Z", optional "v").
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	v, _, _ = strings.Cut(v, "+")
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
