package main

import (
	"testing"
	"time"
)

func TestParseVersionAndOrder(t *testing.T) {
	ordered := []string{"0.9.0", "1.0.0-rc.1", "v1.0.0", "1.0.1", "1.2", "v1.10.0", "2.0.0"}
	for i := 1; i < len(ordered); i++ {
		a, ok1 := parseVersion(ordered[i-1])
		b, ok2 := parseVersion(ordered[i])
		if !ok1 || !ok2 || a.cmp(b) >= 0 {
			t.Errorf("%s should sort before %s", ordered[i-1], ordered[i])
		}
	}
	for _, bad := range []string{"", "v", "1.2.3.4", "1.x", "codeql-bundle-v2.27.3", "1.2.3-", "created"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("parseVersion(%q) should fail", bad)
		}
	}
	if v, _ := parseVersion("v4"); v.cmp(version{major: 4}) != 0 {
		t.Error("v4 should equal 4.0.0")
	}
	if v, _ := parseVersion("1.2.3+build.5"); !v.stable() || v.patch != 3 {
		t.Error("build metadata should be ignored")
	}
}

func TestChooseAppliesCooloff(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	rels := []release{
		{"1.2.0", ago(500)}, // current
		{"1.2.1", ago(100)}, // cleared
		{"1.3.0", ago(30)},  // cleared, highest that has
		{"1.4.0", ago(2)},   // too new
		{"1.5.0-rc.1", ago(50)},
		{"2.0.0", ago(400)}, // newer major: reported, never chosen
	}
	c := choose("1.2.0", rels, cutoff, false)
	if c.Target == nil || c.Target.Version != "1.3.0" {
		t.Fatalf("target = %+v, want 1.3.0", c.Target)
	}
	if c.Newest == nil || c.Newest.Version != "1.4.0" {
		t.Errorf("newest = %+v, want 1.4.0", c.Newest)
	}
	if c.NewerMajor == nil || c.NewerMajor.Version != "2.0.0" {
		t.Errorf("newer major = %+v, want 2.0.0", c.NewerMajor)
	}

	held := choose("1.3.0", rels, cutoff, false)
	if held.Target != nil || held.Newest == nil || held.Newest.Version != "1.4.0" {
		t.Errorf("held: %+v, want no target and newest 1.4.0", held)
	}
	if up := choose("2.0.0", rels, cutoff, false); up.Target != nil || up.Newest != nil || up.NewerMajor != nil {
		t.Errorf("already latest: %+v, want empty choice", up)
	}
}

// TestChooseByVersionNotDate: codeql-action publishes v3.x and v4.x releases
// interleaved (v3.39.1 came out after v4.39.1). Selection must be by version
// within the pinned major, not by publish date.
func TestChooseByVersionNotDate(t *testing.T) {
	now := time.Now()
	rels := []release{
		{"v4.39.1", now.Add(-48 * time.Hour)},
		{"v3.39.1", now.Add(-47 * time.Hour)},
		{"v4.39.0", now.Add(-200 * time.Hour)},
	}
	c := choose("v4.39.0", rels, now.Add(-24*time.Hour), false)
	if c.Target == nil || c.Target.Version != "v4.39.1" || c.NewerMajor != nil {
		t.Fatalf("choice = %+v, want target v4.39.1 and no newer major", c)
	}
}

func TestChooseZeroMinorLines(t *testing.T) {
	cutoff := time.Now()
	rels := []release{{"0.37.0", cutoff.Add(-time.Hour)}}
	if c := choose("0.36.0", rels, cutoff, false); c.Target == nil {
		t.Error("Go-style: 0.36 -> 0.37 should be a normal update")
	}
	if c := choose("0.36.0", rels, cutoff, true); c.Target != nil || c.NewerMajor == nil {
		t.Error("npm-style: 0.36 -> 0.37 should be a major bump")
	}
}

func TestGoToolchainLines(t *testing.T) {
	cur, _ := goKey("go1.27.3")
	next, _ := goKey("go1.27.4")
	minor, _ := goKey("go1.28.0")
	cutoff := time.Now()
	c := choose(cur, []release{{next, cutoff.Add(-time.Hour)}, {minor, cutoff.Add(-time.Hour)}}, cutoff, false)
	if c.Target == nil || c.Target.Version != next {
		t.Errorf("target = %+v, want go1.27.4", c.Target)
	}
	if c.NewerMajor == nil || c.NewerMajor.Version != minor {
		t.Errorf("go1.28 should be reported as a line bump, got %+v", c.NewerMajor)
	}
	if _, ok := goKey("go2.0.0"); ok {
		t.Error("go2 should not parse")
	}
}

func TestEscapeModPath(t *testing.T) {
	if got := escapeModPath("github.com/BurntSushi/toml"); got != "github.com/!burnt!sushi/toml" {
		t.Errorf("escapeModPath = %q", got)
	}
}

func TestParseUses(t *testing.T) {
	ref, ok := parseUses("        uses: github/codeql-action/init@9b3254085494b55a0f7c4c2b3d2d52cc712ce370 # v4.39.1")
	if !ok || ref.Repo != "github/codeql-action" || ref.Path != "/init" || ref.Tag != "v4.39.1" ||
		ref.SHA != "9b3254085494b55a0f7c4c2b3d2d52cc712ce370" {
		t.Errorf("parseUses = %+v %v", ref, ok)
	}
	for _, l := range []string{"uses: actions/checkout@v7", "uses: ./local-action", "run: echo uses: x/y@abc"} {
		if _, ok := parseUses(l); ok {
			t.Errorf("parseUses(%q) should not match", l)
		}
	}
}

func TestParseFromAndStages(t *testing.T) {
	d := "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12"
	ref, ok := parseFrom("FROM cgr.dev/chainguard/go:latest-dev@" + d + " AS build")
	if !ok || ref.Image != "cgr.dev/chainguard/go:latest-dev" || ref.Digest != d {
		t.Errorf("parseFrom = %+v %v", ref, ok)
	}
	if ref, ok := parseFrom("from --platform=linux/amd64 golang:1.27 as b"); !ok || ref.Image != "golang:1.27" || ref.Digest != "" {
		t.Errorf("parseFrom platform/unpinned = %+v %v", ref, ok)
	}
	stages := map[string]bool{"build": true}
	if isImage("build", stages) || isImage("scratch", nil) || !isImage("cgr.dev/chainguard/static:latest", stages) {
		t.Error("isImage misclassified a stage, scratch or image")
	}
}

func TestRequiredVersion(t *testing.T) {
	mod := "module modernc.org/sqlite\n\nrequire (\n\tgolang.org/x/sys v0.49.0\n\tmodernc.org/libc v1.79.0\n)\n\nrequire modernc.org/memory v1.12.1 // indirect\n"
	if got := requiredVersion(mod, "modernc.org/libc"); got != "v1.79.0" {
		t.Errorf("libc = %q", got)
	}
	if got := requiredVersion(mod, "modernc.org/memory"); got != "v1.12.1" {
		t.Errorf("single-line require = %q", got)
	}
	if got := requiredVersion(mod, "example.com/none"); got != "" {
		t.Errorf("missing dep = %q", got)
	}
}

func TestCollapseFamilies(t *testing.T) {
	var rows []row
	for i, p := range []string{"linux-x64-gnu", "darwin-arm64", "win32-x64-msvc"} {
		rows = append(rows, row{Status: statusUpdate, Name: "@rolldown/binding-" + p, Current: "1.2.15",
			Candidate: "1.2.16", Age: hours(time.Duration(46-i) * time.Hour), ageD: time.Duration(46-i) * time.Hour})
	}
	rows = append(rows,
		row{Status: statusUpdate, Name: "postcss", Current: "8.5.31", Candidate: "8.5.32"},
		row{Status: statusHeld, Name: "@scope/a-x", Current: "1", Candidate: "2"}, // pair: too small to fold
		row{Status: statusHeld, Name: "@scope/a-y", Current: "1", Candidate: "2"})
	out := collapseFamilies(rows)
	if len(out) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(out), out)
	}
	if out[0].Name != "@rolldown/binding-* (3 packages)" || out[0].Age != "44h" {
		t.Errorf("family row = %+v, want 3 packages aged 44h (youngest)", out[0])
	}
}
