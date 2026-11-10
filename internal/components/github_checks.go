// github_checks is the "Verify" gate: it waits for the repository's own CI /
// check-runs on a ref (the branch/commit the coding agent produced) and passes
// only when every completed check succeeded. This turns an opened pull request
// from "an artifact exists" into "the repo's checks actually pass" — the evidence
// Superplane's Verify stage is built around. A failure routes the run to a human
// (Changes Requested) with the failing check names attached.
package components

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"conduit/internal/model"
)

type checkRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | neutral | cancelled | timed_out | action_required | skipped
}

// execGitHubChecks polls GitHub's check-runs for a ref until they complete.
// Config: owner, repo, ref (branch or commit SHA; e.g. ${memory.commit_sha} or
// ${memory.work_branch}), token (${secret:github_token}), timeout_s (default 300),
// require_checks ("false" lets a ref with zero checks pass; default true also
// passes when none are configured — see below), api_base (enterprise).
func execGitHubChecks(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(f string) string { return expandTemplate(run, secretExpand(project, n.Field(f))) }

	owner, repo := normalizeOwnerRepo(ex("owner"), ex("repo"))
	ref := firstNonEmpty(ex("ref"), ex("commit"), ex("head"))
	if owner == "" || repo == "" || strings.Contains(owner, "${") || strings.Contains(repo, "${") {
		return failChecks("owner and repo are required")
	}
	if ref == "" || strings.Contains(ref, "${") {
		return failChecks("ref is required (e.g. ${memory.commit_sha} or ${memory.work_branch})")
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "store a secret named github_token"},
			Logs:   []string{"github_checks: no token"}}
	}

	timeout := 300
	if v, e := strconv.Atoi(n.Field("timeout_s")); e == nil && v > 0 {
		timeout = v
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs", githubBase(n), owner, repo, url.PathEscape(ref))
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	var runs []checkRun
	for {
		raw, status, err := getWithHeader(endpoint, githubHeaders(token))
		if err != nil {
			return failChecks(fmt.Sprintf("could not reach GitHub: %v", err))
		}
		if status < 200 || status >= 300 {
			return failChecks(fmt.Sprintf("github %d: %s", status, apiErrSnippet(raw)))
		}
		var body struct {
			Total int        `json:"total_count"`
			Runs  []checkRun `json:"check_runs"`
		}
		_ = json.Unmarshal(raw, &body)
		runs = body.Runs

		// No checks configured for this ref → nothing to verify. Pass unless the
		// node explicitly requires at least one check to exist.
		if body.Total == 0 {
			if strings.EqualFold(n.Field("require_checks"), "true") {
				if time.Now().Before(deadline) {
					time.Sleep(5 * time.Second)
					continue // give CI a moment to register
				}
				return failChecks("no checks reported for ref (require_checks=true)")
			}
			return Result{Outcome: model.StatusSuccess,
				Output:       map[string]any{"ref": ref, "total": 0, "note": "no checks configured"},
				MemoryWrites: map[string]any{"checks_pass": true, "checks_summary": "no checks configured for " + ref},
				Logs:         []string{"github_checks: no checks configured — passing"}}
		}

		allDone := true
		for _, r := range runs {
			if r.Status != "completed" {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		if !time.Now().Before(deadline) {
			return failChecks(fmt.Sprintf("checks did not complete within %ds", timeout))
		}
		time.Sleep(5 * time.Second)
	}

	var failing []string
	passed := 0
	for _, r := range runs {
		switch r.Conclusion {
		case "success", "neutral", "skipped":
			passed++
		default: // failure, cancelled, timed_out, action_required, ""
			failing = append(failing, fmt.Sprintf("%s (%s)", r.Name, firstNonEmpty(r.Conclusion, "incomplete")))
		}
	}
	out := map[string]any{"ref": ref, "total": len(runs), "passed": passed, "failing": failing}
	if len(failing) == 0 {
		return Result{Outcome: model.StatusSuccess, Output: out,
			MemoryWrites: map[string]any{"checks_pass": true, "checks_summary": fmt.Sprintf("%d/%d checks passed", passed, len(runs))},
			Logs:         []string{fmt.Sprintf("github_checks: PASS — %d/%d checks green", passed, len(runs))}}
	}
	summary := "failing: " + strings.Join(failing, ", ")
	return Result{Outcome: model.StatusFailed, Output: out,
		MemoryWrites: map[string]any{"checks_pass": false, "checks_summary": summary},
		Logs:         []string{"github_checks: FAILED — " + summary}}
}

func failChecks(msg string) Result {
	return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": msg}, Logs: []string{"github_checks: " + msg}}
}
