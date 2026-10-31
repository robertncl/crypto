package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaticServingIsConfined builds a dist dir with an asset, plus a secret
// outside it reachable by "..", an absolute-form request, and a symlink, and
// checks that only dist content is ever served.
func TestStaticServingIsConfined(t *testing.T) {
	base := t.TempDir()
	dist := filepath.Join(base, "dist")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dist, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html>SPA</html>"), 0o644))
	must(os.WriteFile(filepath.Join(dist, "assets", "app-abc.js"), []byte("console.log(1)"), 0o644))
	must(os.WriteFile(filepath.Join(base, "secret.txt"), []byte("TOP-SECRET"), 0o644))
	if err := os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(dist, "leak.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	srv := newTestServer(t)
	srv.cfg.WebDir = dist
	srv.Router = srv.routes()
	get := func(target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.Path = target // bypass client-side cleaning: send the raw path
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, r)
		return w
	}

	if w := get("/assets/app-abc.js"); w.Code != 200 || w.Body.String() != "console.log(1)" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q cache=%q", w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
	}
	if w := get("/markets/BTC-USDT"); w.Code != 200 || !strings.Contains(w.Body.String(), "SPA") {
		t.Errorf("SPA fallback: %d %q", w.Code, w.Body.String())
	}
	for _, p := range []string{"/../secret.txt", "/assets/../../secret.txt", "/leak.txt", "/assets/../leak.txt"} {
		w := get(p)
		if strings.Contains(w.Body.String(), "TOP-SECRET") {
			t.Errorf("%s leaked a file outside dist", p)
		}
	}
}

// TestStaticHeadRequest keeps the HEAD route working for health probes.
func TestStaticHeadRequest(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	srv.cfg.WebDir = dist
	srv.Router = srv.routes()
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD / = %d, want 200", w.Code)
	}
}
