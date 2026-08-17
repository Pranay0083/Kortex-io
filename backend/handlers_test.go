package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testApp(t *testing.T, static bool) *App {
	t.Helper()
	cfg := Config{ShardCount: 4, VNodes: 16, NodeID: 1, JWTSecret: strings.Repeat("s", 32), StaticDir: "/nonexistent",
		CORSOrigins: parseOrigins("", "https://kortex.example"), CookieSecure: true, NodeName: "test-node"}
	if static {
		dir := t.TempDir()
		_ = os.MkdirAll(filepath.Join(dir, "img"), 0o755)
		for name, body := range map[string]string{"index.html": "<html>spa</html>", "favicon.ico": "ICO", "robots.txt": "User-agent: *",
			"favicon.svg": "<svg/>", "manifest.webmanifest": `{"name":"kortex"}`, "img/hero.png": "PNG"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_ = os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("js"), 0o644)
		cfg.StaticDir = dir
	}
	return newApp(cfg)
}

func asUser(r *http.Request, id int64, role string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userCtxKey, &User{ID: id, Role: role}))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("bad json %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestLabRingClamped(t *testing.T) {
	a := testApp(t, false)
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/lab/ring?from=100000&to=-3&vnodes=99999999&samples=0", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	m := decode(t, rec)
	if m["vnodes"].(float64) != 1000 || m["samples"].(float64) != 1 {
		t.Fatalf("vnodes=%v samples=%v", m["vnodes"], m["samples"])
	}
	// before/after shard maps reflect the clamped 64 -> 1 resize
	if len(m["after"].(map[string]any)) != 1 {
		t.Fatalf("after %v", m["after"])
	}
}

func TestLabBloomClamped(t *testing.T) {
	a := testApp(t, false)
	h := a.routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/lab/bloom/reset", strings.NewReader(`{"expected": 2000000000, "fp_rate": 1e-300}`)))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	want := NewBloom(labMaxExpected, labMinFPRate).Stats().M
	if got := uint64(decode(t, rec)["stats"].(map[string]any)["m"].(float64)); got != want {
		t.Fatalf("m = %d, want %d", got, want)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/lab/bloom/sweep?inserts=0&probes=-1&fp_rate=0.5", nil))
	m := decode(t, rec)
	if m["inserts"].(float64) != 1 || m["probes"].(float64) != 1 {
		t.Fatalf("sweep %v", m)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/lab/bloom/add", strings.NewReader(`{"key":"`+strings.Repeat("k", maxBody)+`"}`)))
	if rec.Code != 400 {
		t.Fatalf("oversized body status %d", rec.Code)
	}
}

// Run with -race: the playground filter is shared and swapped concurrently.
func TestLabBloomConcurrent(t *testing.T) {
	a := testApp(t, false)
	h := a.routes()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				var r *http.Request
				switch (i + j) % 4 {
				case 0:
					r = httptest.NewRequest("POST", "/api/lab/bloom/reset", strings.NewReader(`{"expected":100}`))
				case 1:
					r = httptest.NewRequest("POST", "/api/lab/bloom/add", strings.NewReader(`{"key":"k"}`))
				case 2:
					r = httptest.NewRequest("GET", "/api/lab/bloom/test?key=k", nil)
				default:
					r = httptest.NewRequest("GET", "/api/lab/bloom", nil)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, r)
				if rec.Code != 200 {
					t.Errorf("status %d", rec.Code)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestCORS(t *testing.T) {
	h := corsMiddleware(parseOrigins("https://app.example, http://localhost:3000", ""))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	for origin, allowed := range map[string]bool{"https://app.example": true, "http://localhost:3000": true, "https://evil.example": false, "": false} {
		r := httptest.NewRequest("GET", "/api/links", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		acao := rec.Header().Get("Access-Control-Allow-Origin")
		if allowed && (acao != origin || rec.Header().Get("Access-Control-Allow-Credentials") != "true") {
			t.Errorf("%q should be allowed, got ACAO=%q", origin, acao)
		}
		if !allowed && (acao != "" || rec.Header().Get("Access-Control-Allow-Credentials") != "") {
			t.Errorf("%q should be refused, got ACAO=%q", origin, acao)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Errorf("missing Vary: Origin")
		}
	}
	r := httptest.NewRequest("OPTIONS", "/api/links", nil)
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("preflight %d %v", rec.Code, rec.Header())
	}
}

func TestRootRouting(t *testing.T) {
	h := testApp(t, true).routes()
	cases := []struct {
		method, path, accept string
		status               int
		body                 string
	}{
		{"GET", "/", "text/html", 200, "spa"},
		{"GET", "/login", "text/html", 200, "spa"},
		{"GET", "/architecture", "", 200, "spa"},
		{"HEAD", "/register", "text/html", 200, ""},
		{"GET", "/favicon.ico", "", 200, "ICO"},
		{"GET", "/robots.txt", "", 200, "User-agent"},
		{"GET", "/assets/app.js", "", 200, "js"},
		{"GET", "/favicon.svg", "", 200, "<svg/>"},
		{"GET", "/manifest.webmanifest", "", 200, "kortex"},
		{"GET", "/img/hero.png", "", 200, "PNG"},
		{"HEAD", "/favicon.svg", "", 200, ""},
		{"GET", "/app/links/abc", "text/html", 200, "spa"},
		{"GET", "/assets/missing.js", "", 404, ""},
		{"GET", "/no.such.code", "text/html,application/xhtml+xml", 404, "spa"},
		{"GET", "/no.such.code", "application/json", 404, "unknown short code"},
		{"GET", "/api/nope", "text/html", 404, "not found"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s %s: %d %q", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	for _, s := range []string{"login", "register", "app", "lab", "architecture", "favicon", "manifest", "robots"} {
		if !reservedAliases[s] {
			t.Errorf("%q should be reserved", s)
		}
	}
}

func TestBenchRequiresAdmin(t *testing.T) {
	a := testApp(t, false)
	rec := httptest.NewRecorder()
	a.handleBenchStart(rec, asUser(httptest.NewRequest("POST", "/api/bench/start", strings.NewReader(`{}`)), 2, "user"))
	if rec.Code != 403 {
		t.Fatalf("start as user: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	a.handleBenchStop(rec, asUser(httptest.NewRequest("POST", "/api/bench/stop", nil), 2, "user"))
	if rec.Code != 403 {
		t.Fatalf("stop as user: %d", rec.Code)
	}
	a.bench.running.Store(true)
	rec = httptest.NewRecorder()
	a.handleBenchStart(rec, asUser(httptest.NewRequest("POST", "/api/bench/start", strings.NewReader(`{}`)), 1, "admin"))
	if rec.Code != 409 {
		t.Fatalf("second bench: %d", rec.Code)
	}
}

func TestStreamScopedAndNoIP(t *testing.T) {
	a := testApp(t, false)
	a.bus = NewBroker("clicks", 1, 100, func([]ClickEvent) {})
	for i := 0; i < 10; i++ {
		a.bus.pushRecent(ClickEvent{TS: int64(i), Code: "c", UserID: int64(i%2 + 1)})
	}
	rec := httptest.NewRecorder()
	a.handleStream(rec, asUser(httptest.NewRequest("GET", "/api/stream/recent?limit=100", nil), 2, "user"))
	evs := decode(t, rec)["events"].([]any)
	if len(evs) != 5 {
		t.Fatalf("user saw %d events", len(evs))
	}
	for _, e := range evs {
		ev := e.(map[string]any)
		if ev["user_id"] != "2" {
			t.Fatalf("leaked event %v", ev)
		}
		if _, ok := ev["ip"]; ok {
			t.Fatal("event exposes ip")
		}
	}
	rec = httptest.NewRecorder()
	a.handleStream(rec, asUser(httptest.NewRequest("GET", "/api/stream/recent", nil), 1, "admin"))
	if n := len(decode(t, rec)["events"].([]any)); n != 10 {
		t.Fatalf("admin saw %d events", n)
	}
}

func TestBreakdownRejectsUnknownDim(t *testing.T) {
	a := testApp(t, false)
	rec := httptest.NewRecorder()
	a.handleBreakdown(rec, asUser(httptest.NewRequest("GET", "/api/analytics/all/breakdown?dim=password", nil), 1, "user"))
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestTokensAndCookies(t *testing.T) {
	a := testApp(t, false)
	u := &User{ID: 42, Email: "a@b.co", Role: "user"}
	rec := httptest.NewRecorder()
	a.emitAuth(rec, u)
	body := decode(t, rec)
	if len(body) != 1 || body["user"] == nil {
		t.Fatalf("auth body must only carry the user: %v", body)
	}
	cookies := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c
	}
	acc, ref := cookies[accessCookie], cookies[refreshCookie]
	if acc == nil || ref == nil {
		t.Fatal("missing cookies")
	}
	for _, c := range []*http.Cookie{acc, ref} {
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie %s flags: %+v", c.Name, c)
		}
	}
	if acc.Path != "/" || ref.Path != "/api/auth" {
		t.Errorf("paths access=%q refresh=%q", acc.Path, ref.Path)
	}

	c, err := a.parseToken(acc.Value, "access")
	if err != nil || c.Subject != "42" || c.ID == "" {
		t.Fatalf("access token: %+v %v", c, err)
	}
	if _, err := a.parseToken(acc.Value, "refresh"); err == nil {
		t.Fatal("access token accepted as refresh")
	}
	rc, err := a.parseToken(ref.Value, "refresh")
	if err != nil || rc.ID == c.ID {
		t.Fatalf("refresh token: %v (jti reuse=%v)", err, rc != nil && rc.ID == c.ID)
	}

	forge := func(m jwt.SigningMethod, key any, iss string) string {
		tok, _ := jwt.NewWithClaims(m, Claims{Kind: "access", RegisteredClaims: jwt.RegisteredClaims{
			ID: "x", Subject: "42", Issuer: iss, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}).SignedString(key)
		return tok
	}
	for name, tok := range map[string]string{
		"wrong issuer": forge(jwt.SigningMethodHS256, []byte(a.cfg.JWTSecret), "evil"),
		"wrong key":    forge(jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32)), tokenIssuer),
		"alg none":     forge(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, tokenIssuer),
		"hs512":        forge(jwt.SigningMethodHS512, []byte(a.cfg.JWTSecret), tokenIssuer),
	} {
		if _, err := a.parseToken(tok, "access"); err == nil {
			t.Errorf("%s token accepted", name)
		}
	}
	if good := forge(jwt.SigningMethodHS256, []byte(a.cfg.JWTSecret), tokenIssuer); good == "" {
		t.Fatal("forge helper broken")
	} else if _, err := a.parseToken(good, "access"); err != nil {
		t.Fatalf("well-formed token rejected: %v", err)
	}

	rec = httptest.NewRecorder()
	a.clearAuthCookies(rec)
	for _, ck := range rec.Result().Cookies() {
		if ck.MaxAge >= 0 {
			t.Errorf("cookie %s not cleared", ck.Name)
		}
	}
}

func TestLogoutWithoutSession(t *testing.T) {
	rec := httptest.NewRecorder()
	testApp(t, false).routes().ServeHTTP(rec, httptest.NewRequest("POST", "/api/auth/logout", nil))
	if rec.Code != 200 {
		t.Fatalf("logout status %d", rec.Code)
	}
	if n := len(rec.Result().Cookies()); n < 2 {
		t.Fatalf("expected cookies to be cleared, got %d Set-Cookie", n)
	}
}

func TestDecodeLabRoundTrip(t *testing.T) {
	a := testApp(t, false)
	id := a.ids.Next()
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/lab/snowflake?code="+encodeCode(id), nil))
	parts := decode(t, rec)["parts"].(map[string]any)
	if parts["id"] != itoa(id) || decode(t, rec)["plausible"] != true {
		t.Fatalf("decoded %v", parts)
	}
}

func TestBenchAliasPrefixIsReserved(t *testing.T) {
	for _, s := range []string{"bench-01", "Bench-16", "bench-anything", "login"} {
		if !isReservedAlias(s) {
			t.Errorf("%q should be reserved", s)
		}
	}
	for _, s := range []string{"benchmark", "my-bench-01", "k6"} {
		if isReservedAlias(s) {
			t.Errorf("%q should be allowed", s)
		}
	}
}

func TestBenchKeepsLastResultPerKind(t *testing.T) {
	b := NewBench()
	run := func(kind string, rate float64) {
		b.running.Store(true)
		b.mu.Lock()
		b.snap = BenchSnapshot{Kind: kind, Running: true, RPS: rate, IngestRate: rate, Log: []string{"x"}}
		b.mu.Unlock()
		b.finish()
	}
	run("ingest", 1000)
	run("redirect", 50)
	s := b.Snapshot()
	if s.Kind != "redirect" || s.Running || s.Phase != "done" {
		t.Fatalf("current run not finished: %+v", s)
	}
	if s.LastIngest == nil || s.LastIngest.IngestRate != 1000 {
		t.Fatalf("ingest result lost after a redirect run: %+v", s.LastIngest)
	}
	if s.LastRedirect == nil || s.LastRedirect.RPS != 50 || s.LastRedirect.Log != nil {
		t.Fatalf("last redirect wrong or carries its log: %+v", s.LastRedirect)
	}
	if b.running.Load() {
		t.Fatal("still marked running")
	}
}
