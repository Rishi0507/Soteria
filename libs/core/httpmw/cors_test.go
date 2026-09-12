package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestCORSAllowsAConfiguredOrigin(t *testing.T) {
	h := CORS("http://localhost:5173", handler())
	r := httptest.NewRequest(http.MethodGet, "/v1/lots/status", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("allow-origin = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin so caches cannot cross origins", got)
	}
	if w.Body.String() != "ok" {
		t.Errorf("request must still reach the handler, got %q", w.Body.String())
	}
}

func TestCORSIgnoresAnUnlistedOrigin(t *testing.T) {
	h := CORS("http://localhost:5173", handler())
	r := httptest.NewRequest(http.MethodGet, "/v1/lots/status", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin = %q, want empty for an unlisted origin", got)
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	h := CORS("*", handler())
	r := httptest.NewRequest(http.MethodOptions, "/v1/rescues/r-1/confirm", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("preflight must not reach the handler, body = %q", w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("preflight must advertise the allowed headers")
	}
}

// TestCORSDisabledByDefault: behind a gateway that serves everything on one
// origin, the right answer is no CORS headers at all.
func TestCORSDisabledByDefault(t *testing.T) {
	h := CORS("", handler())
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin = %q, want empty when unconfigured", got)
	}
	if w.Body.String() != "ok" {
		t.Error("an unconfigured middleware must still pass requests through")
	}
}
