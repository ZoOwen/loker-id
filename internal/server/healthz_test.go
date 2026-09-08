package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleHealthz_ReturnsOKWhenDBReachable(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body[status] = %q, want %q", body["status"], "ok")
	}
}

// TestHandleHealthz_ReturnsServiceUnavailableWhenDBUnreachable stands in
// for a DB that's actually down (rather than just cold-starting): closing
// the pool makes every subsequent Ping fail the same way an unreachable
// database would, without needing a real broken connection.
func TestHandleHealthz_ReturnsServiceUnavailableWhenDBUnreachable(t *testing.T) {
	ts, _, pool, _ := newTestServer(t)
	defer ts.Close()
	pool.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] == "ok" {
		t.Errorf("body[status] = %q, want something other than ok", body["status"])
	}
}
