// security_scan gates a run on an external security / vulnerability scan (the
// agency-audit service). It starts a scan, polls for the structured verdict, and
// returns success when the verdict passes (no High/Critical findings and no SQL
// injection) or failed otherwise — so a workflow can block a merge on it.
package components

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"conduit/internal/model"
)

type scanVerdict struct {
	Critical      int    `json:"critical"`
	High          int    `json:"high"`
	Medium        int    `json:"medium"`
	Low           int    `json:"low"`
	SQLInjection  bool   `json:"sql_injection"`
	Summary       string `json:"summary"`
	Pass          bool   `json:"pass"`
	BlockedReason string `json:"blocked_reason"`
}

// execSecurityScan runs the pre-merge security gate. Config:
//
//	base_url   scan service (default https://launchaudit.io; or ${var:audit_url})
//	token      ${secret:scan_token} by default (X-Scan-Token)
//	repo_url   the repo to scan, or owner+repo (built into a github.com URL)
//	ref        informational branch/sha
//	timeout_s  max seconds to wait for the verdict (default 300)
func execSecurityScan(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(f string) string { return expandTemplate(run, secretExpand(project, n.Field(f))) }

	base := ex("base_url")
	if base == "" || strings.Contains(base, "${") {
		base = "https://launchaudit.io"
	}
	base = strings.TrimRight(base, "/")
	repoURL := ex("repo_url")
	if repoURL == "" || strings.Contains(repoURL, "${") {
		owner, repo, _ := prContextFromConfigOrMemory(run, ex("owner"), ex("repo"), "")
		if owner != "" && repo != "" {
			repoURL = "https://github.com/" + owner + "/" + repo
		}
	}
	if repoURL == "" || strings.Contains(repoURL, "${") {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "repo_url (or owner + repo) is required"},
			Logs:   []string{"security_scan: repo_url (or owner + repo) is required"}}
	}
	token := secretExpand(project, firstNonEmpty(n.Field("token"), "${secret:scan_token}"))
	token = strings.TrimSpace(token)
	if token == "" || strings.Contains(token, "${secret:") {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no scan token", "hint": "add a secret named scan_token (matches the service's SCAN_API_TOKEN)"},
			Logs:   []string{"security_scan: no scan_token secret"}}
	}

	ref := ex("ref")
	if strings.Contains(ref, "${") {
		ref = memoryString(run, "pr_head")
	}
	hdr := map[string]string{"X-Scan-Token": token}

	// Start the scan.
	raw, status, err := postJSON(base+"/api/scan", hdr, map[string]any{"repo_url": repoURL, "ref": ref})
	if err != nil {
		return failScan(fmt.Sprintf("could not reach scan service: %v", err))
	}
	if status < 200 || status >= 300 {
		return failScan(fmt.Sprintf("scan service %d: %s", status, apiErrSnippet(raw)))
	}
	var started struct {
		JobID string `json:"job_id"`
	}
	_ = json.Unmarshal(raw, &started)
	if started.JobID == "" {
		return failScan("scan service did not return a job id")
	}

	// Poll for the verdict.
	timeout := 300
	if v, e := strconv.Atoi(n.Field("timeout_s")); e == nil && v > 0 {
		timeout = v
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		poll, pstatus, perr := getWithHeader(base+"/api/scan/"+started.JobID, hdr)
		if perr != nil || pstatus != 200 {
			continue // transient; keep polling until the deadline
		}
		var jr struct {
			Status  string       `json:"status"`
			Verdict *scanVerdict `json:"verdict"`
			Report  string       `json:"report_markdown"`
			Error   string       `json:"error"`
		}
		_ = json.Unmarshal(poll, &jr)
		switch jr.Status {
		case "done":
			return scanResult(repoURL, started.JobID, jr.Verdict, jr.Report)
		case "failed":
			return failScan("scan failed: " + firstNonEmpty(jr.Error, "unknown error"))
		}
	}
	return failScan(fmt.Sprintf("scan did not finish within %ds", timeout))
}

func scanResult(repoURL, jobID string, v *scanVerdict, report string) Result {
	if v == nil {
		return failScan("scan returned no verdict")
	}
	preview := report
	if len(preview) > 6000 {
		preview = preview[:6000] + "\n… (report truncated)"
	}
	out := map[string]any{
		"repo":          repoURL,
		"job_id":        jobID,
		"pass":          v.Pass,
		"critical":      v.Critical,
		"high":          v.High,
		"medium":        v.Medium,
		"low":           v.Low,
		"sql_injection": v.SQLInjection,
		"summary":       v.Summary,
	}
	mem := map[string]any{
		"security_pass":    v.Pass,
		"security_summary": v.Summary,
		"security_report":  preview,
	}
	if v.Pass {
		return Result{Outcome: model.StatusSuccess, Output: out, MemoryWrites: mem,
			Logs: []string{fmt.Sprintf("security_scan: PASS (%d critical / %d high, sqli=%v)", v.Critical, v.High, v.SQLInjection)}}
	}
	out["blocked_reason"] = v.BlockedReason
	return Result{Outcome: model.StatusFailed, Output: out, MemoryWrites: mem,
		Logs: []string{fmt.Sprintf("security_scan: BLOCKED — %s", firstNonEmpty(v.BlockedReason, "high/critical or SQL injection findings"))}}
}

func failScan(msg string) Result {
	return Result{Outcome: model.StatusFailed,
		Output: map[string]any{"error": msg},
		Logs:   []string{"security_scan: " + msg}}
}

// getWithHeader does a GET with custom headers using the standard client.
func getWithHeader(url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := liveHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return out, resp.StatusCode, nil
}
