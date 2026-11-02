package components

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"conduit/internal/model"
)

func scanNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "scan", Type: "security_scan", Config: cfg}
}

func TestSecurityScanMissingRepo(t *testing.T) {
	r := execSecurityScan(scanNode(map[string]string{"token": "t"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed with no repo, got %s", r.Outcome)
	}
}

func TestSecurityScanMissingToken(t *testing.T) {
	// Unresolved ${secret:…} must be treated as no token, not a literal.
	r := execSecurityScan(scanNode(map[string]string{"repo_url": "https://github.com/a/b"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed with no token, got %s", r.Outcome)
	}
}

func TestSecurityScanPassAndBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    scanVerdict
		want model.Status
	}{
		{"clean", scanVerdict{Pass: true, Summary: "clean"}, model.StatusSuccess},
		{"high", scanVerdict{High: 2, BlockedReason: "2 high", Summary: "found 2"}, model.StatusFailed},
		{"sqli", scanVerdict{SQLInjection: true, BlockedReason: "SQL injection finding"}, model.StatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("X-Scan-Token") != "sekret" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if req.Method == http.MethodPost {
					_ = json.NewEncoder(w).Encode(map[string]any{"job_id": "job1"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "done", "verdict": tc.v, "report_markdown": "# report",
				})
			}))
			defer srv.Close()

			r := execSecurityScan(scanNode(map[string]string{
				"base_url": srv.URL, "repo_url": "https://github.com/a/b",
				"token": "sekret", "timeout_s": "30",
			}), &model.Run{})
			if r.Outcome != tc.want {
				t.Fatalf("%s: want %s, got %s (%v)", tc.name, tc.want, r.Outcome, r.Output)
			}
			if r.MemoryWrites["security_report"] == nil {
				t.Errorf("%s: expected security_report in memory", tc.name)
			}
		})
	}
}

func TestSecurityScanServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"detail":"upstream down"}`))
	}))
	defer srv.Close()
	r := execSecurityScan(scanNode(map[string]string{
		"base_url": srv.URL, "repo_url": "https://github.com/a/b", "token": "t",
	}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed on 502, got %s", r.Outcome)
	}
}
