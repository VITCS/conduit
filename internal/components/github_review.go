// Agent-driven PR review and merge. execGitHubReview fetches a pull request's real
// diff from GitHub and asks an LLM to review it, writing the verdict into run
// memory (so a later step — e.g. github_comment with ${memory.review} — can post
// it). execGitHubMerge merges a PR; in the seeded workflow it runs only AFTER a
// human approval gate, which is the safety control for a real side effect.
package components

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"conduit/internal/model"
)

const maxDiffChars = 45000

// githubDo performs a GitHub API request with auth + standard headers. accept lets
// callers ask for the raw diff media type; payload (when non-nil) is sent as JSON.
func githubDo(method, url, token, accept string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", githubUserAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := liveHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return out, resp.StatusCode, nil
}

// execGitHubReview reviews a real PR with an LLM. Required: owner, repo, number.
// Optional: provider/model/api_key/max_tokens (live review; falls back to a clear
// simulated review when no api_key), token (${secret:github_token} by default),
// api_base.
func execGitHubReview(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(f string) string { return expandTemplate(run, secretExpand(project, n.Field(f))) }

	owner, repo, number := prContextFromConfigOrMemory(run, ex("owner"), ex("repo"), firstNonEmpty(ex("number"), ex("pr")))
	if owner == "" || repo == "" || number == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "owner, repo and number are required"},
			Logs:   []string{"github_review: owner, repo and number are required"}}
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "add a github_token secret"},
			Logs:   []string{"github_review: no github_token — cannot fetch the PR"}}
	}
	base := githubBase(n)

	// PR metadata (title, size) for context.
	var meta struct {
		Title        string `json:"title"`
		Additions    int    `json:"additions"`
		Deletions    int    `json:"deletions"`
		ChangedFiles int    `json:"changed_files"`
		User         struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	metaRaw, status, err := githubDo(http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%s", base, owner, repo, number), token, "", nil)
	if err != nil || status != 200 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": githubError(metaRaw, status), "repo": owner + "/" + repo},
			Logs:   []string{fmt.Sprintf("github_review: fetch PR %s#%s -> %d", owner+"/"+repo, number, status)}}
	}
	_ = json.Unmarshal(metaRaw, &meta)

	// Raw unified diff.
	diffRaw, dstatus, derr := githubDo(http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/pulls/%s", base, owner, repo, number), token, "application/vnd.github.diff", nil)
	if derr != nil || dstatus != 200 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": dstatus, "error": "could not fetch diff"},
			Logs:   []string{fmt.Sprintf("github_review: fetch diff -> %d", dstatus)}}
	}
	diff := string(diffRaw)
	truncated := false
	if len(diff) > maxDiffChars {
		diff = diff[:maxDiffChars]
		truncated = true
	}
	// A bounded preview of the diff, surfaced in the node output + run memory so a
	// reviewer can see the changes alongside the review at the approval gate.
	diffPreview := diff
	if len(diffPreview) > 6000 {
		diffPreview = diffPreview[:6000] + "\n… (diff truncated)"
	}

	apiKey := secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("apikey")))
	// provider/model may also come from project config (${var:…}); default sensibly
	// and ignore any unresolved reference so a missing var can't become a bad model.
	provider := ex("provider")
	if provider == "" || strings.Contains(provider, "${") {
		provider = "anthropic"
	}
	modelName := ex("model")
	if strings.Contains(modelName, "${") {
		modelName = ""
	}

	// No LLM key → a clear, simulated review so the flow is still demonstrable.
	if apiKey == "" || strings.Contains(apiKey, "${secret:") {
		review := fmt.Sprintf("CONFIDENCE: medium\n[simulated review] %q (+%d/-%d, %d files). Set an api_key secret and a provider on this node for a real LLM review.",
			meta.Title, meta.Additions, meta.Deletions, meta.ChangedFiles)
		return Result{
			Outcome: model.StatusSuccess,
			Output:  map[string]any{"simulated": true, "repo": owner + "/" + repo, "number": number, "title": meta.Title, "confidence": "medium", "review": review, "diff_preview": diffPreview},
			Logs:    []string{"github_review: no api_key — produced a simulated review"},
			MemoryWrites: map[string]any{
				"review": review, "review_confidence": "medium", "diff_preview": diffPreview,
				"pr_owner": owner, "pr_name": repo, "pr_repo": owner + "/" + repo, "pr_number": number,
				"pr_head": meta.Head.Ref,
			},
		}
	}

	prompt := buildReviewPrompt(owner+"/"+repo, meta.Title, meta.User.Login, meta.Additions, meta.Deletions, meta.ChangedFiles, diff, truncated)
	maxTokens := 4096
	if v, e := strconv.Atoi(n.Field("max_tokens")); e == nil && v > 0 {
		maxTokens = v
	}
	text, _, lerr := callLLM(provider, n.Field("base_url"), apiKey, modelName, prompt, maxTokens)
	if lerr != nil || strings.TrimSpace(text) == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": fmt.Sprintf("llm review failed: %v", lerr)},
			Logs:   []string{fmt.Sprintf("github_review: llm error: %v", lerr)}}
	}
	confidence := parseConfidence(text)
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"repo": owner + "/" + repo, "number": number, "title": meta.Title, "confidence": confidence, "review": text, "diff_preview": diffPreview},
		Logs:    []string{fmt.Sprintf("github_review: reviewed %s#%s (confidence %s)", owner+"/"+repo, number, confidence)},
		MemoryWrites: map[string]any{
			"review": text, "review_confidence": confidence, "diff_preview": diffPreview,
			"pr_owner": owner, "pr_name": repo, "pr_repo": owner + "/" + repo, "pr_number": number,
			"pr_head": meta.Head.Ref,
		},
	}
}

func buildReviewPrompt(repo, title, author string, add, del, files int, diff string, truncated bool) string {
	note := ""
	if truncated {
		note = "\n(The diff was truncated for length; review what is shown.)"
	}
	return fmt.Sprintf(`You are a senior engineer reviewing a GitHub pull request.

Repository: %s
Title: %s
Author: %s
Change size: +%d / -%d across %d files.%s

Review the unified diff below for correctness, security, and obvious bugs. Be concise and specific (reference files/lines). Note anything risky.
Begin your reply with exactly one line "CONFIDENCE: high" or "CONFIDENCE: medium" or "CONFIDENCE: low", then the review.

DIFF:
%s`, repo, title, author, add, del, files, note, diff)
}

func parseConfidence(text string) string {
	first := strings.ToLower(strings.TrimSpace(strings.SplitN(text, "\n", 2)[0]))
	switch {
	case strings.Contains(first, "confidence") && strings.Contains(first, "high"):
		return "high"
	case strings.Contains(first, "confidence") && strings.Contains(first, "low"):
		return "low"
	case strings.Contains(first, "confidence") && strings.Contains(first, "medium"):
		return "medium"
	}
	return "unknown"
}

// execGitHubMerge merges a PR. Required: owner, repo, number. Optional: method
// (merge|squash|rebase, default squash), commit_title, commit_message, token,
// api_base. Intended to run after a human approval gate.
func execGitHubMerge(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(f string) string { return expandTemplate(run, secretExpand(project, n.Field(f))) }

	owner, repo, number := prContextFromConfigOrMemory(run, ex("owner"), ex("repo"), firstNonEmpty(ex("number"), ex("pr")))
	if owner == "" || repo == "" || number == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "owner, repo and number are required"},
			Logs:   []string{"github_merge: owner, repo and number are required"}}
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "add a github_token secret with Contents: write"},
			Logs:   []string{"github_merge: no github_token"}}
	}
	method := firstNonEmpty(strings.ToLower(n.Field("method")), "squash")
	payload := map[string]any{"merge_method": method}
	if t := ex("commit_title"); t != "" {
		payload["commit_title"] = t
	}
	if m := ex("commit_message"); m != "" {
		payload["commit_message"] = m
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%s/merge", githubBase(n), owner, repo, number)
	raw, status, err := githubDo(http.MethodPut, url, token, "", payload)
	if err != nil {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": err.Error()},
			Logs:   []string{fmt.Sprintf("github_merge: PUT %s -> error: %v", url, err)}}
	}
	if status != 200 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": githubError(raw, status), "repo": owner + "/" + repo, "number": number},
			Logs:   []string{fmt.Sprintf("github_merge: %s#%s -> %d: %s", owner+"/"+repo, number, status, githubError(raw, status))}}
	}
	var r struct {
		Merged  bool   `json:"merged"`
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &r)
	return Result{
		Outcome:      model.StatusCreated,
		Output:       map[string]any{"merged": r.Merged, "sha": r.SHA, "method": method, "repo": owner + "/" + repo, "number": number},
		Logs:         []string{fmt.Sprintf("github_merge: merged %s#%s (%s) %s", owner+"/"+repo, number, method, r.SHA)},
		MemoryWrites: map[string]any{"merged_sha": r.SHA, "merged_pr": owner + "/" + repo + "#" + number},
	}
}
