// Real GitHub actions: open a pull request and comment on an issue/PR via the
// GitHub REST API. Unlike the simulated components, these perform real side
// effects against api.github.com (or a GitHub Enterprise base URL), authenticated
// with a project-scoped token secret. They are the "end-to-end against a real
// system" path: configure a token and a repo, and a run opens an actual PR.
package components

import (
	"encoding/json"
	"fmt"
	"strings"

	"conduit/internal/model"
)

const (
	githubAPIVersion = "2022-11-28"
	githubUserAgent  = "conduit-control-plane"
)

// githubToken resolves the auth token for a GitHub node: an explicit `token`
// field, else the conventional `${secret:github_token}` secret. Returns the token
// and whether it resolved to a real (non-placeholder) value.
func githubToken(n model.Node, project string) (string, bool) {
	tok := secretExpand(project, firstNonEmpty(n.Field("token"), "${secret:github_token}"))
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.Contains(tok, "${secret:") {
		return "", false
	}
	return tok, true
}

func githubHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization":        "Bearer " + token,
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": githubAPIVersion,
		"User-Agent":           githubUserAgent,
	}
}

func githubBase(n model.Node) string {
	return strings.TrimRight(firstNonEmpty(n.Field("api_base"), "https://api.github.com"), "/")
}

// githubError extracts a human-readable message from a GitHub error response body.
func githubError(raw []byte, status int) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	for _, d := range e.Errors {
		if d.Message != "" {
			msg += ": " + d.Message
		} else if d.Code != "" {
			msg += ": " + d.Code
		}
	}
	return msg
}

// execGitHubPR opens a real pull request. Required config: owner, repo, head
// (branch with the changes), title. Optional: base (default "main"), body, draft,
// token (default ${secret:github_token}), api_base (for GitHub Enterprise).
// Config values support ${secret:NAME}, ${input.KEY} and ${memory.KEY}.
func execGitHubPR(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }

	owner, repo := ex("owner"), ex("repo")
	head := ex("head")
	base := firstNonEmpty(ex("base"), "main")
	title := ex("title")
	body := ex("body")

	if owner == "" || repo == "" || head == "" || title == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "owner, repo, head and title are required"},
			Logs:   []string{"github_pr: owner, repo, head and title are required"}}
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "store a secret named github_token (or set the node's token field)"},
			Logs:   []string{"github_pr: no token — add a 'github_token' secret with repo/PR write access"}}
	}

	payload := map[string]any{"title": title, "head": head, "base": base, "body": body}
	if strings.EqualFold(n.Field("draft"), "true") {
		payload["draft"] = true
	}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", githubBase(n), owner, repo)
	raw, status, err := postJSON(url, githubHeaders(token), payload)
	if err != nil {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": err.Error(), "repo": owner + "/" + repo},
			Logs:   []string{fmt.Sprintf("github_pr: POST %s -> error: %v", url, err)}}
	}
	if status != 201 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": githubError(raw, status), "repo": owner + "/" + repo},
			Logs:   []string{fmt.Sprintf("github_pr: %s#%s -> %d: %s", owner+"/"+repo, head, status, githubError(raw, status))}}
	}

	var pr struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
	}
	_ = json.Unmarshal(raw, &pr)
	out := map[string]any{
		"number":   pr.Number,
		"html_url": pr.HTMLURL,
		"state":    pr.State,
		"repo":     owner + "/" + repo,
		"head":     head,
		"base":     base,
	}
	return Result{
		Outcome: model.StatusCreated,
		Output:  out,
		Logs:    []string{fmt.Sprintf("github_pr: opened %s#%d — %s", owner+"/"+repo, pr.Number, pr.HTMLURL)},
		MemoryWrites: map[string]any{
			fmt.Sprintf("pr/%s/%s", owner, repo): out,
			"last_pr_url":                        pr.HTMLURL,
			"last_pr_number":                     pr.Number,
		},
	}
}

// execGitHubComment posts a real comment on an issue or pull request. Required
// config: owner, repo, number, body. Optional: token, api_base. Config values
// support ${secret:NAME}, ${input.KEY} and ${memory.KEY} (so a webhook's PR
// number flows straight in as ${input.number}).
func execGitHubComment(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }

	owner, repo := ex("owner"), ex("repo")
	number := firstNonEmpty(ex("number"), ex("issue"), ex("pr"))
	body := ex("body")
	if owner == "" || repo == "" || number == "" || body == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "owner, repo, number and body are required"},
			Logs:   []string{"github_comment: owner, repo, number and body are required"}}
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token"},
			Logs:   []string{"github_comment: no token — add a 'github_token' secret"}}
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s/comments", githubBase(n), owner, repo, number)
	raw, status, err := postJSON(url, githubHeaders(token), map[string]any{"body": body})
	if err != nil {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": err.Error()},
			Logs:   []string{fmt.Sprintf("github_comment: POST %s -> error: %v", url, err)}}
	}
	if status != 201 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": githubError(raw, status)},
			Logs:   []string{fmt.Sprintf("github_comment: %s#%s -> %d: %s", owner+"/"+repo, number, status, githubError(raw, status))}}
	}
	var c struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
	}
	_ = json.Unmarshal(raw, &c)
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"html_url": c.HTMLURL, "id": c.ID, "repo": owner + "/" + repo, "number": number},
		Logs:    []string{fmt.Sprintf("github_comment: commented on %s#%s — %s", owner+"/"+repo, number, c.HTMLURL)},
	}
}
