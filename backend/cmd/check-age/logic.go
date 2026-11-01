package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// version is a parsed semantic version. Only stable releases (no pre-release
// suffix) are ever chosen as update targets.
type version struct {
	major, minor, patch int
	pre                 string // pre-release suffix without the leading "-"
}

// parseVersion accepts "1.2.3", "v1.2.3", "v1.2.3-rc.1" and "v1.2.3+meta".
// Missing minor/patch parts default to 0 ("v4" == "v4.0.0").
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre, s = s[i+1:], s[:i]
		if v.pre == "" {
			return version{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return version{}, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" {
			return version{}, false
		}
		nums[i] = n
	}
	v.major, v.minor, v.patch = nums[0], nums[1], nums[2]
	return v, true
}

func (v version) stable() bool { return v.pre == "" }

// cmp orders versions per semver precedence (pre-release sorts before the
// release; pre-release identifiers compare as plain strings, which is enough
// to never pick one over a release).
func (v version) cmp(w version) int {
	for _, d := range [3]int{v.major - w.major, v.minor - w.minor, v.patch - w.patch} {
		if d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.pre == w.pre:
		return 0
	case v.pre == "":
		return 1
	case w.pre == "":
		return -1
	}
	return strings.Compare(v.pre, w.pre)
}

// line is the compatibility line a version belongs to: its major version, or
// with zeroMinor, for 0.x its minor too. npm treats 0.x minor bumps as
// breaking (^0.2.3 excludes 0.3.0); Go does not (go get -u takes them).
func (v version) line(zeroMinor bool) [2]int {
	if zeroMinor && v.major == 0 {
		return [2]int{0, v.minor}
	}
	return [2]int{v.major, -1}
}

// release is one published version and when it was published.
type release struct {
	Version string
	Time    time.Time
}

// choice is the outcome of applying the cooloff to a dependency's releases.
type choice struct {
	Target     *release // highest same-line release that has cleared the cooloff
	Newest     *release // highest same-line release, regardless of age
	NewerMajor *release // highest release on a newer line (never auto-chosen)
}

// choose applies the cooloff: among stable releases newer than current, the
// target is the highest one on current's compatibility line published at or
// before cutoff. Newer lines (major bumps) are reported but never targeted.
// zeroMinor selects npm-style lines for 0.x versions (see version.line).
func choose(current string, rels []release, cutoff time.Time, zeroMinor bool) choice {
	cur, ok := parseVersion(current)
	if !ok {
		return choice{}
	}
	var c choice
	for i := range rels {
		r := &rels[i]
		v, ok := parseVersion(r.Version)
		if !ok || !v.stable() || v.cmp(cur) <= 0 {
			continue
		}
		higher := func(best *release) bool {
			if best == nil {
				return true
			}
			b, _ := parseVersion(best.Version)
			return v.cmp(b) > 0
		}
		if v.line(zeroMinor) != cur.line(zeroMinor) {
			if higher(c.NewerMajor) {
				c.NewerMajor = r
			}
			continue
		}
		if higher(c.Newest) {
			c.Newest = r
		}
		if !r.Time.After(cutoff) && higher(c.Target) {
			c.Target = r
		}
	}
	return c
}

// escapeModPath applies the Go module proxy's case encoding: each upper-case
// letter becomes "!" followed by its lower-case form.
func escapeModPath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// goRelease parses a Go toolchain version such as "go1.27.3" or "1.27" into
// a version whose line is the Go minor release (1.27), since each Go minor is
// its own language version and moving between them is a deliberate bump.
func goRelease(s string) (version, bool) {
	s = strings.TrimPrefix(s, "go")
	v, ok := parseVersion(s)
	if !ok || v.major != 1 {
		return version{}, false
	}
	// Re-key so line() groups by minor: major := 1000+minor.
	return version{major: 1000 + v.minor, minor: v.patch, pre: v.pre}, true
}

// goKey renders a toolchain version in the form choose() compares.
func goKey(s string) (string, bool) {
	v, ok := goRelease(s)
	if !ok {
		return "", false
	}
	k := strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) + ".0"
	if v.pre != "" {
		k += "-" + v.pre
	}
	return k, true
}

// actionRef is one SHA-pinned `uses:` reference in a workflow.
type actionRef struct {
	Repo string // owner/name
	Path string // sub-action path, e.g. "/init" ("" for the repo root)
	SHA  string
	Tag  string // version from the trailing "# vX.Y.Z" comment
}

var usesRe = regexp.MustCompile(`uses:\s*([\w.-]+/[\w.-]+)(/[^@\s]*)?@([0-9a-f]{40})\s*#\s*(v?\d[\w.-]*)`)

// parseUses extracts a SHA-pinned action reference from a workflow line.
func parseUses(line string) (actionRef, bool) {
	m := usesRe.FindStringSubmatch(line)
	if m == nil {
		return actionRef{}, false
	}
	return actionRef{Repo: m[1], Path: m[2], SHA: m[3], Tag: m[4]}, true
}

// imageRef is one FROM line in a Dockerfile.
type imageRef struct {
	Image  string // registry/repo:tag
	Digest string // "sha256:..." or "" when not digest-pinned
}

var fromRe = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--platform=\S+\s+)?([^\s@]+)(?:@(sha256:[0-9a-f]{64}))?`)

// parseFrom extracts the image of a FROM line. References to earlier build
// stages and "scratch" are not images and are skipped by the caller via
// isImage.
func parseFrom(line string) (imageRef, bool) {
	m := fromRe.FindStringSubmatch(line)
	if m == nil {
		return imageRef{}, false
	}
	return imageRef{Image: m[1], Digest: m[2]}, true
}

// isImage reports whether a FROM target names a registry image rather than an
// earlier stage (stages are bare names) or scratch.
func isImage(name string, stages map[string]bool) bool {
	if strings.EqualFold(name, "scratch") || stages[strings.ToLower(name)] {
		return false
	}
	return strings.ContainsAny(name, "/:.")
}

// requiredVersion returns the version of dep required in a go.mod body.
func requiredVersion(goMod, dep string) string {
	for _, l := range strings.Split(goMod, "\n") {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "require "))
		if len(f) >= 2 && f[0] == dep {
			return f[1]
		}
	}
	return ""
}

// hours renders an age as whole hours, the unit the cooloff is reasoned in.
func hours(d time.Duration) string {
	return strconv.Itoa(int(d.Hours())) + "h"
}
