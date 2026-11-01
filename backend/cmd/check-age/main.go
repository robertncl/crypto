// Command check-age reports dependency updates that have cleared a release
// cooloff (default 24h), across everything this repo pins:
//
//   - the Go toolchain and the modules listed in backend/go.mod
//   - npm packages in web/ (direct, plus transitive lockfile updates)
//   - GitHub Actions pinned by commit SHA in .github/workflows
//   - container base images pinned by digest in the Dockerfiles
//
// A release younger than the cooloff is reported as HELD and never proposed,
// so a compromised or broken release has a day to be caught and yanked before
// it lands here. Major-version bumps are reported but never proposed. The
// modernc.org/libc version is pinned to whatever modernc.org/sqlite requires
// (see pairedWith). The tool only reads; it never edits files.
//
// Usage, from the repository root:
//
//	make check-age
//	cd backend && go run ./cmd/check-age -root .. [-cooloff 24h] [-skip docker,actions] [-fail-on-update]
//
// It shells out to go, npm, gh (for Actions) and docker (for images); a
// section whose tool is missing is skipped with a note.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// pairedWith lists modules whose version must equal what another module's
// go.mod requires. modernc.org/sqlite documents that it must be used with the
// exact modernc.org/libc version in its own go.mod ("Fragile modernc.org/libc
// dependency", sqlite doc.go; https://gitlab.com/cznic/sqlite/-/issues/177).
var pairedWith = map[string]string{
	"modernc.org/libc": "modernc.org/sqlite",
}

const (
	statusOK     = "OK"
	statusUpdate = "UPDATE"
	statusHeld   = "HELD"
	statusSkip   = "SKIP"
	statusInfo   = "INFO"
)

type row struct {
	Status, Name, Current, Candidate, Age, Note string

	ageD time.Duration // Age as a duration, for comparisons
}

type section struct {
	Title string
	Rows  []row
	Hints []string // commands or edits that apply the UPDATE rows
}

type checker struct {
	root    string
	cutoff  time.Time
	now     time.Time
	http    *http.Client
	goproxy string
}

func main() {
	root := flag.String("root", "", "repository root (default: nearest directory containing .git)")
	cooloff := flag.Duration("cooloff", 24*time.Hour, "minimum age of a release before it is proposed")
	skip := flag.String("skip", "", "comma-separated sections to skip: toolchain,go,npm,actions,docker")
	failOnUpdate := flag.Bool("fail-on-update", false, "exit 1 when any update is available")
	flag.Parse()

	r := *root
	if r == "" {
		var err error
		if r, err = findRoot(); err != nil {
			fmt.Fprintln(os.Stderr, "check-age:", err)
			os.Exit(2)
		}
	}
	now := time.Now().UTC()
	c := &checker{root: r, now: now, cutoff: now.Add(-*cooloff), http: &http.Client{Timeout: 30 * time.Second}}
	c.goproxy = c.goProxy()

	skipped := map[string]bool{}
	for _, s := range strings.Split(*skip, ",") {
		skipped[strings.TrimSpace(s)] = true
	}
	type step struct {
		key string
		fn  func() section
	}
	var out []section
	for _, s := range []step{
		{"toolchain", c.goToolchain}, {"go", c.goModules}, {"npm", c.npmDirect},
		{"npm", c.npmTransitive}, {"actions", c.actions}, {"docker", c.images},
	} {
		if !skipped[s.key] {
			out = append(out, s.fn())
		}
	}

	fmt.Printf("check-age: cooloff %s (releases published before %s UTC)\n", *cooloff, c.cutoff.Format("2006-01-02 15:04"))
	updates, held := 0, 0
	for _, s := range out {
		fmt.Printf("\n== %s\n", s.Title)
		if len(s.Rows) == 0 {
			fmt.Println("   (nothing pinned)")
			continue
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, r := range s.Rows {
			switch r.Status {
			case statusUpdate:
				updates++
			case statusHeld:
				held++
			}
			fmt.Fprintf(tw, "   %s\t%s\t%s\t%s\t%s\t%s\n", r.Status, r.Name, r.Current, arrow(r.Candidate), r.Age, r.Note)
		}
		_ = tw.Flush()
		for _, h := range s.Hints {
			fmt.Println("   ->", h)
		}
	}
	fmt.Printf("\n%d update(s) cleared the cooloff, %d held back.\n", updates, held)
	if *failOnUpdate && updates > 0 {
		os.Exit(1)
	}
}

func arrow(candidate string) string {
	if candidate == "" {
		return ""
	}
	return "-> " + candidate
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no .git found above the working directory; pass -root")
		}
		dir = parent
	}
}

// ---------- helpers ----------

// run executes a command (no shell) in dir and returns its stdout.
func run(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (c *checker) getJSON(url string, v any) error {
	resp, err := c.http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}

func (c *checker) getText(url string) (string, error) {
	resp, err := c.http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

func (c *checker) age(t time.Time) string { return hours(c.now.Sub(t)) }

// fromChoice turns a cooloff decision into a report row.
func (c *checker) fromChoice(name, current string, ch choice, display func(string) string) row {
	if display == nil {
		display = func(s string) string { return s }
	}
	r := row{Name: name, Current: display(current)}
	switch {
	case ch.Target != nil:
		r.Status, r.Candidate, r.Age = statusUpdate, display(ch.Target.Version), c.age(ch.Target.Time)
		if ch.Newest != nil && ch.Newest.Version != ch.Target.Version {
			r.Note = fmt.Sprintf("newest %s is only %s old", display(ch.Newest.Version), c.age(ch.Newest.Time))
		}
	case ch.Newest != nil:
		r.Status, r.Candidate, r.Age = statusHeld, display(ch.Newest.Version), c.age(ch.Newest.Time)
		r.Note = "inside the cooloff"
	default:
		r.Status = statusOK
	}
	if ch.NewerMajor != nil {
		r.Note = strings.TrimPrefix(r.Note+"; ", "; ") + fmt.Sprintf("major %s available (review manually)", display(ch.NewerMajor.Version))
	}
	return r
}

// ---------- Go ----------

func (c *checker) goProxy() string {
	out, err := run(c.root, "go", "env", "GOPROXY")
	if err == nil {
		for _, p := range strings.FieldsFunc(strings.TrimSpace(string(out)), func(r rune) bool { return r == ',' || r == '|' }) {
			if strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "http://") {
				return strings.TrimRight(p, "/")
			}
		}
	}
	return "https://proxy.golang.org"
}

func (c *checker) modInfo(path, ver string) (release, error) {
	var info struct {
		Version string
		Time    time.Time
	}
	err := c.getJSON(c.goproxy+"/"+escapeModPath(path)+"/@v/"+escapeModPath(ver)+".info", &info)
	return release{Version: info.Version, Time: info.Time}, err
}

type goModFile struct {
	Go        string
	Toolchain *struct{ Name string }
	Require   []struct {
		Path, Version string
		Indirect      bool
	}
}

func (c *checker) readGoMod() (goModFile, error) {
	var m goModFile
	out, err := run(filepath.Join(c.root, "backend"), "go", "mod", "edit", "-json")
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(out, &m)
}

func (c *checker) goToolchain() section {
	s := section{Title: "Go toolchain (backend/go.mod)"}
	mod, err := c.readGoMod()
	if err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go", Note: err.Error()})
		return s
	}
	current := "go" + mod.Go
	if mod.Toolchain != nil {
		if a, ok1 := goRelease(mod.Toolchain.Name); ok1 {
			if b, ok2 := goRelease(current); !ok2 || a.cmp(b) > 0 {
				current = mod.Toolchain.Name
			}
		}
	}
	var dl []struct {
		Version string
		Stable  bool
	}
	if err := c.getJSON("https://go.dev/dl/?mode=json&include=all", &dl); err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go", Current: current, Note: err.Error()})
		return s
	}
	cur, ok := goRelease(current)
	if !ok {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go", Current: current, Note: "unparseable go directive"})
		return s
	}
	keyToName := map[string]string{}
	var rels []release
	for _, d := range dl {
		v, ok := goRelease(d.Version)
		if !ok || !d.Stable || v.cmp(cur) <= 0 {
			continue
		}
		key, _ := goKey(d.Version)
		keyToName[key] = d.Version
		info, err := c.modInfo("golang.org/toolchain", "v0.0.1-"+d.Version+".linux-amd64")
		if err != nil {
			continue
		}
		rels = append(rels, release{Version: key, Time: info.Time})
	}
	curKey, _ := goKey(current)
	keyToName[curKey] = current
	r := c.fromChoice("go", curKey, choose(curKey, rels, c.cutoff, false), func(k string) string {
		if n, ok := keyToName[k]; ok {
			return n
		}
		return k
	})
	s.Rows = append(s.Rows, r)
	if r.Status == statusUpdate {
		s.Hints = append(s.Hints, fmt.Sprintf("cd backend && go mod edit -go=%s", strings.TrimPrefix(r.Candidate, "go")))
	}
	return s
}

func (c *checker) goModules() section {
	s := section{Title: "Go modules (backend/go.mod)"}
	mod, err := c.readGoMod()
	if err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go.mod", Note: err.Error()})
		return s
	}
	var paths []string
	indirect := map[string]bool{}
	for _, r := range mod.Require {
		paths = append(paths, r.Path)
		indirect[r.Path] = r.Indirect
	}
	sort.Strings(paths)
	out, err := run(filepath.Join(c.root, "backend"), "go", append([]string{"list", "-m", "-u", "-json"}, paths...)...)
	if err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go list", Note: err.Error()})
		return s
	}
	type listed struct {
		Path, Version string
		Update        *struct {
			Version string
			Time    time.Time
		}
	}
	rows := map[string]*row{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var m listed
		if err := dec.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			s.Rows = append(s.Rows, row{Status: statusSkip, Name: "go list", Note: err.Error()})
			return s
		}
		var ch choice
		if m.Update != nil {
			cur, okc := parseVersion(m.Version)
			upd, oku := parseVersion(m.Update.Version)
			if okc && oku && cur.stable() && upd.stable() {
				// Tagged releases: walk every newer version so an older one that
				// cleared the cooloff can be proposed when the newest hasn't.
				ch = choose(m.Version, c.goVersionsBetween(m.Path, m.Version), c.cutoff, false)
			}
			if ch.Newest == nil && ch.NewerMajor == nil {
				// Pseudo-versions (which choose can't order) or a failed lookup:
				// go list's own answer is the only candidate.
				u := release{Version: m.Update.Version, Time: m.Update.Time}
				ch = choice{Newest: &u}
				if !u.Time.After(c.cutoff) {
					ch.Target = &u
				}
			}
		}
		r := c.fromChoice(m.Path, m.Version, ch, nil)
		if indirect[m.Path] {
			r.Note = strings.TrimPrefix(r.Note+"; indirect", "; ")
		}
		rows[m.Path] = &r
	}

	// Paired modules follow their anchor's requirement, not their own releases.
	for dep, anchor := range pairedWith {
		dr, ar := rows[dep], rows[anchor]
		if dr == nil || ar == nil {
			continue
		}
		anchorVer := ar.Current
		if ar.Status == statusUpdate {
			anchorVer = ar.Candidate
		}
		body, err := c.getText(c.goproxy + "/" + escapeModPath(anchor) + "/@v/" + escapeModPath(anchorVer) + ".mod")
		req := requiredVersion(body, dep)
		if err != nil || req == "" {
			dr.Status, dr.Note = statusSkip, fmt.Sprintf("could not read %s@%s requirement for it", anchor, anchorVer)
			continue
		}
		note := fmt.Sprintf("pinned to %s@%s's requirement", anchor, anchorVer)
		if req == dr.Current {
			dr.Status, dr.Candidate, dr.Age, dr.Note = statusOK, "", "", note
		} else {
			dr.Status, dr.Candidate, dr.Age, dr.Note = statusUpdate, req, "", note
			if ar.Status == statusUpdate {
				ar.Note = strings.TrimPrefix(ar.Note+"; ", "; ") + fmt.Sprintf("move %s to %s with it", dep, req)
			}
		}
	}

	var gets []string
	for _, p := range paths {
		r := rows[p]
		if r == nil {
			continue
		}
		s.Rows = append(s.Rows, *r)
		if r.Status == statusUpdate {
			gets = append(gets, p+"@"+r.Candidate)
		}
	}
	if len(gets) > 0 {
		s.Hints = append(s.Hints, "cd backend && go get "+strings.Join(gets, " ")+" && go mod tidy")
	}
	s.Hints = append(s.Hints, "modules only in the build graph (not in go.mod) are not listed")
	return s
}

// goVersionsBetween returns the published releases of path newer than current,
// with their times, from the module proxy.
func (c *checker) goVersionsBetween(path, current string) []release {
	cur, _ := parseVersion(current)
	body, err := c.getText(c.goproxy + "/" + escapeModPath(path) + "/@v/list")
	if err != nil {
		return nil
	}
	var rels []release
	for _, v := range strings.Fields(body) {
		pv, ok := parseVersion(v)
		if !ok || !pv.stable() || pv.cmp(cur) <= 0 {
			continue
		}
		if info, err := c.modInfo(path, v); err == nil {
			rels = append(rels, info)
		}
	}
	return rels
}

// ---------- npm ----------

type pkgJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type lockJSON struct {
	Packages map[string]struct {
		Version string `json:"version"`
	} `json:"packages"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// npmTimes returns a package's version -> publish time map.
func npmTimes(dir, name string, cache map[string]map[string]time.Time) (map[string]time.Time, error) {
	if t, ok := cache[name]; ok {
		return t, nil
	}
	out, err := run(dir, "npm", "view", name, "time", "--json")
	if err != nil {
		return nil, err
	}
	var raw map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	t := map[string]time.Time{}
	for v, ts := range raw {
		if p, err := time.Parse(time.RFC3339, ts); err == nil {
			t[v] = p
		}
	}
	cache[name] = t
	return t, nil
}

func (c *checker) npmDirect() section {
	s := section{Title: "npm packages (web/package.json)"}
	web := filepath.Join(c.root, "web")
	var pkg pkgJSON
	var lock lockJSON
	if err := readJSON(filepath.Join(web, "package.json"), &pkg); err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "package.json", Note: err.Error()})
		return s
	}
	_ = readJSON(filepath.Join(web, "package-lock.json"), &lock)
	specs := map[string]string{}
	for k, v := range pkg.Dependencies {
		specs[k] = v
	}
	for k, v := range pkg.DevDependencies {
		specs[k] = v
	}
	names := make([]string, 0, len(specs))
	for n := range specs {
		names = append(names, n)
	}
	sort.Strings(names)
	cache := map[string]map[string]time.Time{}
	var installs []string
	for _, name := range names {
		spec := specs[name]
		current := strings.TrimLeft(spec, "^~=")
		if l, ok := lock.Packages["node_modules/"+name]; ok && l.Version != "" {
			current = l.Version
		}
		times, err := npmTimes(web, name, cache)
		if err != nil {
			s.Rows = append(s.Rows, row{Status: statusSkip, Name: name, Current: current, Note: err.Error()})
			continue
		}
		rels := make([]release, 0, len(times))
		for v, t := range times {
			rels = append(rels, release{Version: v, Time: t})
		}
		r := c.fromChoice(name, current, choose(current, rels, c.cutoff, true), nil)
		if strings.ContainsAny(spec, "^~") {
			r.Note = strings.TrimPrefix(r.Note+"; ", "; ") + "range " + spec
		}
		s.Rows = append(s.Rows, r)
		if r.Status == statusUpdate {
			installs = append(installs, name+"@"+r.Candidate)
		}
	}
	if len(installs) > 0 {
		s.Hints = append(s.Hints, "cd web && npm install --save-exact --ignore-scripts "+strings.Join(installs, " "))
	}
	return s
}

// npmTransitive dry-runs `npm update` on a copy of the lockfile and reports
// which transitive packages would move, and whether each clears the cooloff.
func (c *checker) npmTransitive() section {
	s := section{Title: "npm transitive updates (web/package-lock.json)"}
	web := filepath.Join(c.root, "web")
	tmp, err := os.MkdirTemp("", "check-age-npm-")
	if err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "lockfile", Note: err.Error()})
		return s
	}
	defer os.RemoveAll(tmp)
	for _, f := range []string{"package.json", "package-lock.json"} {
		b, err := os.ReadFile(filepath.Join(web, f))
		if err == nil {
			err = os.WriteFile(filepath.Join(tmp, f), b, 0o600)
		}
		if err != nil {
			s.Rows = append(s.Rows, row{Status: statusSkip, Name: f, Note: err.Error()})
			return s
		}
	}
	var before, after lockJSON
	var pkg pkgJSON
	_ = readJSON(filepath.Join(tmp, "package-lock.json"), &before)
	_ = readJSON(filepath.Join(tmp, "package.json"), &pkg)
	if _, err := run(tmp, "npm", "update", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund"); err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "npm update", Note: err.Error()})
		return s
	}
	if err := readJSON(filepath.Join(tmp, "package-lock.json"), &after); err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "lockfile", Note: err.Error()})
		return s
	}
	direct := map[string]bool{}
	for n := range pkg.Dependencies {
		direct["node_modules/"+n] = true
	}
	for n := range pkg.DevDependencies {
		direct["node_modules/"+n] = true
	}
	type change struct{ name, from, to string }
	seen := map[string]bool{}
	var changes []change
	for key, a := range after.Packages {
		if !strings.HasPrefix(key, "node_modules/") || direct[key] {
			continue
		}
		from := before.Packages[key].Version
		if from == a.Version {
			continue
		}
		name := key[strings.LastIndex(key, "node_modules/")+len("node_modules/"):]
		if id := name + "@" + a.Version; !seen[id] {
			seen[id] = true
			changes = append(changes, change{name, from, a.Version})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].name < changes[j].name })
	cache := map[string]map[string]time.Time{}
	var ok []string
	var rows []row
	for _, ch := range changes {
		r := row{Name: ch.name, Current: ch.from, Candidate: ch.to}
		if ch.from == "" {
			r.Current, r.Note = "(new)", "newly added to the tree"
		}
		times, err := npmTimes(web, ch.name, cache)
		t, found := times[ch.to]
		switch {
		case err != nil || !found:
			r.Status, r.Note = statusSkip, "publish time unknown"
		case t.After(c.cutoff):
			r.Status, r.Age, r.ageD, r.Note = statusHeld, c.age(t), c.now.Sub(t), "inside the cooloff"
		default:
			r.Status, r.Age, r.ageD = statusUpdate, c.age(t), c.now.Sub(t)
			ok = append(ok, ch.name)
		}
		rows = append(rows, r)
	}
	s.Rows = append(s.Rows, collapseFamilies(rows)...)
	if len(ok) > 0 {
		s.Hints = append(s.Hints, "cd web && npm update --package-lock-only --ignore-scripts "+strings.Join(ok, " "))
		if len(ok) < len(changes) {
			s.Hints = append(s.Hints, "afterwards confirm no HELD version slipped in (re-run check-age)")
		}
	}
	return s
}

// collapseFamilies folds per-platform package families that move together
// (e.g. @rolldown/binding-linux-x64-gnu, -darwin-arm64, ...) into one row, so
// a single upstream release is reported — and counted — once.
func collapseFamilies(rows []row) []row {
	type key struct{ family, status, from, to string }
	groups := map[key][]row{}
	var order []key
	for _, r := range rows {
		fam := r.Name
		if scope, rest, ok := strings.Cut(r.Name, "/"); ok && strings.HasPrefix(scope, "@") {
			if head, _, ok := strings.Cut(rest, "-"); ok {
				fam = scope + "/" + head + "-*"
			}
		}
		k := key{fam, r.Status, r.Current, r.Candidate}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	var out []row
	for _, k := range order {
		g := groups[k]
		if len(g) < 3 {
			out = append(out, g...)
			continue
		}
		r := g[0]
		r.Name = fmt.Sprintf("%s (%d packages)", k.family, len(g))
		for _, m := range g[1:] { // report the youngest member's age
			if m.ageD < r.ageD {
				r.Age, r.ageD = m.Age, m.ageD
			}
		}
		out = append(out, r)
	}
	return out
}

// ---------- GitHub Actions ----------

func (c *checker) actions() section {
	s := section{Title: "GitHub Actions (.github/workflows, SHA-pinned)"}
	files, _ := filepath.Glob(filepath.Join(c.root, ".github", "workflows", "*.y*ml"))
	type key struct{ repo, sha, tag string }
	uses := map[key][]string{}
	var unpinned []string
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := sc.Text()
			if ref, ok := parseUses(line); ok {
				k := key{ref.Repo, ref.SHA, ref.Tag}
				uses[k] = append(uses[k], ref.Repo+ref.Path)
			} else if strings.Contains(line, "uses:") && !strings.Contains(line, "uses: ./") {
				unpinned = append(unpinned, strings.TrimSpace(line))
			}
		}
		fh.Close()
	}
	if _, err := exec.LookPath("gh"); err != nil {
		s.Rows = append(s.Rows, row{Status: statusSkip, Name: "gh", Note: "GitHub CLI not found"})
		return s
	}
	keys := make([]key, 0, len(uses))
	for k := range uses {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].repo < keys[j].repo })
	for _, k := range keys {
		r := row{Name: k.repo, Current: k.tag}
		out, err := run(c.root, "gh", "api", "repos/"+k.repo+"/releases?per_page=100",
			"--jq", `.[] | select(.draft|not) | select(.prerelease|not) | [.tag_name, .published_at] | @tsv`)
		if err != nil {
			r.Status, r.Note = statusSkip, err.Error()
			s.Rows = append(s.Rows, r)
			continue
		}
		var rels []release
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Split(l, "\t")
			if len(f) != 2 {
				continue
			}
			if t, err := time.Parse(time.RFC3339, f[1]); err == nil {
				rels = append(rels, release{Version: f[0], Time: t})
			}
		}
		r = c.fromChoice(k.repo, k.tag, choose(k.tag, rels, c.cutoff, false), nil)
		// The comment is only a label; verify the pinned SHA really is that tag.
		if sha, err := run(c.root, "gh", "api", "repos/"+k.repo+"/commits/"+k.tag, "--jq", ".sha"); err == nil {
			if got := strings.TrimSpace(string(sha)); got != k.sha {
				r.Note = strings.TrimPrefix(r.Note+"; ", "; ") + fmt.Sprintf("WARNING: pinned %s is not %s (%s)", k.sha[:12], k.tag, got[:min(12, len(got))])
			}
		}
		if r.Status == statusUpdate {
			if sha, err := run(c.root, "gh", "api", "repos/"+k.repo+"/commits/"+r.Candidate, "--jq", ".sha"); err == nil {
				target := strings.TrimSpace(string(sha))
				for _, u := range dedupe(uses[k]) {
					s.Hints = append(s.Hints, fmt.Sprintf("uses: %s@%s # %s", u, target, r.Candidate))
				}
			}
		}
		s.Rows = append(s.Rows, r)
	}
	for _, u := range dedupe(unpinned) {
		s.Rows = append(s.Rows, row{Status: statusInfo, Name: u, Note: "not pinned to a commit SHA"})
	}
	return s
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// ---------- container images ----------

func (c *checker) images() section {
	s := section{Title: "Container images (Dockerfiles, digest-pinned)"}
	var refs []imageRef
	var where []string
	for _, file := range []string{"Dockerfile", filepath.Join(".devcontainer", "Dockerfile")} {
		b, err := os.ReadFile(filepath.Join(c.root, file))
		if err != nil {
			continue
		}
		stages := map[string]bool{} // earlier "FROM ... AS name" stages in this file
		for _, line := range strings.Split(string(b), "\n") {
			ref, ok := parseFrom(line)
			if !ok {
				continue
			}
			if isImage(ref.Image, stages) {
				refs = append(refs, ref)
				where = append(where, file)
			}
			if fields := strings.Fields(line); len(fields) >= 4 && strings.EqualFold(fields[len(fields)-2], "AS") {
				stages[strings.ToLower(fields[len(fields)-1])] = true
			}
		}
	}
	docker := ""
	for _, name := range []string{"docker", "docker.exe"} {
		if _, err := exec.LookPath(name); err == nil {
			docker = name
			break
		}
	}
	for i, ref := range refs {
		r := row{Name: ref.Image, Note: where[i]}
		if ref.Digest == "" {
			r.Status, r.Note = statusInfo, where[i]+": not digest-pinned"
			s.Rows = append(s.Rows, r)
			continue
		}
		r.Current = ref.Digest[:19]
		if docker == "" {
			r.Status, r.Note = statusSkip, "docker not found"
			s.Rows = append(s.Rows, r)
			continue
		}
		newest, newestAt, err := inspectImage(docker, ref.Image)
		if err != nil {
			r.Status, r.Note = statusSkip, err.Error()
			s.Rows = append(s.Rows, r)
			continue
		}
		_, pinnedAt, _ := inspectImage(docker, ref.Image+"@"+ref.Digest)
		if !pinnedAt.IsZero() {
			r.Current += " (" + pinnedAt.Format("2006-01-02") + ")"
		}
		switch {
		case newest == ref.Digest:
			r.Status = statusOK
		case newestAt.After(c.cutoff):
			r.Status, r.Candidate, r.Age = statusHeld, newest[:19], c.age(newestAt)
			r.Note = "newest build inside the cooloff; an older qualifying digest can't be listed from the registry"
		default:
			r.Status, r.Candidate, r.Age = statusUpdate, newest[:19], c.age(newestAt)
			s.Hints = append(s.Hints, fmt.Sprintf("%s: FROM %s@%s", where[i], ref.Image, newest))
		}
		s.Rows = append(s.Rows, r)
	}
	return s
}

// inspectImage returns the digest an image reference resolves to and its
// build time (linux/amd64 when multi-platform).
func inspectImage(docker, ref string) (string, time.Time, error) {
	out, err := run("", docker, "buildx", "imagetools", "inspect", ref)
	if err != nil {
		return "", time.Time{}, err
	}
	digest := ""
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "Digest:" {
			digest = f[1]
			break
		}
	}
	if digest == "" {
		return "", time.Time{}, fmt.Errorf("no digest for %s", ref)
	}
	out, err = run("", docker, "buildx", "imagetools", "inspect", ref, "--format", "{{json .Image}}")
	if err != nil {
		return digest, time.Time{}, nil
	}
	var multi map[string]struct {
		Created time.Time `json:"created"`
	}
	if json.Unmarshal(out, &multi) == nil {
		if img, ok := multi["linux/amd64"]; ok && !img.Created.IsZero() {
			return digest, img.Created, nil
		}
	}
	var single struct {
		Created time.Time `json:"created"`
	}
	if json.Unmarshal(out, &single) == nil && !single.Created.IsZero() {
		return digest, single.Created, nil
	}
	for _, img := range multi {
		if !img.Created.IsZero() {
			return digest, img.Created, nil
		}
	}
	return digest, time.Time{}, nil
}
