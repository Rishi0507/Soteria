package health

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTracker(now *time.Time) *Tracker {
	t := New("ingestion-test")
	t.started = *now
	t.now = func() time.Time { return *now }
	t.Register("fda_press", time.Minute)
	return t
}

func TestStalenessAndFailureThresholds(t *testing.T) {
	now := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)
	tr := newTracker(&now)

	if _, ok := tr.Snapshot(); !ok {
		t.Fatal("fresh tracker within grace period should be healthy")
	}
	now = now.Add(StaleFactor*time.Minute + time.Second)
	if _, ok := tr.Snapshot(); ok {
		t.Fatal("never-succeeded source past grace period should be unhealthy")
	}

	tr.RecordSuccess("fda_press", 5, 2, 300*time.Millisecond)
	if _, ok := tr.Snapshot(); !ok {
		t.Fatal("healthy right after success")
	}
	for i := 0; i < MaxConsecutiveFailures-1; i++ {
		tr.RecordFailure("fda_press", errors.New("boom"), time.Second, false)
	}
	if _, ok := tr.Snapshot(); !ok {
		t.Fatalf("%d failures should still be healthy", MaxConsecutiveFailures-1)
	}
	tr.RecordFailure("fda_press", errors.New("boom"), time.Second, true)
	snap, ok := tr.Snapshot()
	if ok || !strings.Contains(snap["fda_press"].Reason, "consecutive") {
		t.Fatalf("expected consecutive-failure reason, got ok=%v %+v", ok, snap["fda_press"])
	}
	tr.RecordSuccess("fda_press", 0, 0, time.Second)
	if _, ok := tr.Snapshot(); !ok {
		t.Fatal("success resets failures")
	}
	now = now.Add(StaleFactor*time.Minute + time.Second)
	snap, ok = tr.Snapshot()
	if ok || !strings.Contains(snap["fda_press"].Reason, "exceeds") {
		t.Fatalf("expected staleness, got ok=%v %+v", ok, snap["fda_press"])
	}
}

func TestBrokerCheckAffectsHealth(t *testing.T) {
	now := time.Now()
	tr := newTracker(&now)
	tr.RecordSuccess("fda_press", 1, 1, time.Second)
	connected := true
	tr.SetBrokerCheck(func() bool { return connected })
	if _, ok := tr.Snapshot(); !ok {
		t.Fatal("healthy with broker connected")
	}
	connected = false
	if _, ok := tr.Snapshot(); ok {
		t.Fatal("a lost broker connection must make the service unhealthy even when every feed poll succeeds")
	}
}

func TestEndpoints(t *testing.T) {
	now := time.Now()
	tr := newTracker(&now)
	srv := httptest.NewServer(tr.Handler())
	defer srv.Close()

	get := func(path string) (int, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}

	if code, _ := get("/readyz"); code != 503 {
		t.Errorf("readyz before ready: want 503 got %d", code)
	}
	tr.SetReady(true)
	if code, _ := get("/readyz"); code != 200 {
		t.Errorf("readyz after ready: want 200 got %d", code)
	}

	tr.RecordSuccess("fda_press", 3, 1, time.Second)
	code, body := get("/healthz")
	if code != 200 {
		t.Errorf("healthz: want 200 got %d", code)
	}
	var h struct {
		Healthy bool                   `json:"healthy"`
		Sources map[string]SourceState `json:"sources"`
	}
	if err := json.Unmarshal([]byte(body), &h); err != nil || !h.Healthy || h.Sources["fda_press"].ItemsPublishedTotal != 1 {
		t.Errorf("healthz body: %s err=%v", body, err)
	}

	for i := 0; i < MaxConsecutiveFailures; i++ {
		tr.RecordFailure("fda_press", errors.New("feed 403"), time.Second, false)
	}
	if code, body := get("/healthz"); code != 503 || !strings.Contains(body, "feed 403") {
		t.Errorf("healthz unhealthy: want 503 with last_error, got %d %s", code, body)
	}

	_, metrics := get("/metrics")
	for _, want := range []string{
		`soteria_ingestion_up{producer="ingestion-test"} 0`,
		`soteria_ingestion_consecutive_failures{producer="ingestion-test",source="fda_press"} 3`,
		`soteria_ingestion_items_published_total{producer="ingestion-test",source="fda_press"} 1`,
		"# TYPE soteria_ingestion_fetch_total counter",
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("metrics missing %q\n%s", want, metrics)
		}
	}
}
