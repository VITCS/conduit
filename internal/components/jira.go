// Real Jira actions for the AI Factory: pull a ticket into a run and post a
// comment back. Authenticated with a project-scoped Jira Cloud API token (basic
// auth over email:token). The fetch action injects the ticket's key, summary and
// description into run memory as ticket_* so the downstream agent steps work on a
// real issue; the comment action closes the loop by writing the agent's output
// back onto the ticket. Config values support ${var:…}, ${secret:…}, ${input.…}
// and ${memory.…}.
package components

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"conduit/internal/model"
)

// jiraCreds resolves base URL + basic-auth header for a Jira node. Returns ok=false
// when the token (or email/base) is missing or still a placeholder.
func jiraCreds(n model.Node, project string, ex func(string) string) (base, authHeader string, ok bool) {
	base = strings.TrimRight(ex("base_url"), "/")
	email := ex("email")
	token := secretExpand(project, firstNonEmpty(n.Field("api_token"), n.Field("token"), "${secret:jira_token}"))
	token = strings.TrimSpace(token)
	if base == "" || strings.Contains(base, "${") ||
		email == "" || strings.Contains(email, "${") ||
		token == "" || strings.Contains(token, "${secret:") {
		return "", "", false
	}
	cred := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	return base, "Basic " + cred, true
}

func jiraHeaders(auth string) map[string]string {
	return map[string]string{"Authorization": auth, "Accept": "application/json", "Content-Type": "application/json"}
}

// execJira dispatches on the `action` field: "fetch" (default) pulls a ticket,
// "comment" posts a comment back to a ticket.
func execJira(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }
	if strings.EqualFold(n.Field("action"), "comment") {
		return jiraComment(n, ex, project)
	}
	return jiraFetch(n, ex, project)
}

func jiraFetch(n model.Node, ex func(string) string, project string) Result {
	base, auth, ok := jiraCreds(n, project, ex)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "missing Jira config", "hint": "set vars jira_base + jira_email and a secret named jira_token"},
			Logs:   []string{"jira: missing base_url / email / token"}}
	}

	// A specific key wins; otherwise run JQL and take the first match.
	var endpoint string
	key := ex("issue_key")
	if key != "" && !strings.Contains(key, "${") {
		endpoint = fmt.Sprintf("%s/rest/api/3/issue/%s?fields=summary,description,status", base, url.PathEscape(key))
	} else {
		jql := firstNonEmpty(ex("jql"), "ORDER BY created ASC")
		endpoint = fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=1&fields=summary,description,status",
			base, url.QueryEscape(jql))
	}

	raw, status, err := getWithHeader(endpoint, jiraHeaders(auth))
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()},
			Logs: []string{fmt.Sprintf("jira: GET -> error: %v", err)}}
	}
	if status < 200 || status >= 300 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": apiErrSnippet(raw)},
			Logs:   []string{fmt.Sprintf("jira: fetch -> %d: %s", status, apiErrSnippet(raw))}}
	}

	issue, found := firstJiraIssue(raw)
	if !found {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"found": false, "error": "no matching Jira issue"},
			Logs:   []string{"jira: no matching issue for query"}}
	}

	browseURL := fmt.Sprintf("%s/browse/%s", base, issue.Key)
	mem := map[string]any{
		"ticket_key":         issue.Key,
		"ticket_summary":     issue.Summary,
		"ticket_description": issue.Description,
		"ticket_status":      issue.Status,
		"ticket_url":         browseURL,
		"ticket_source":      "jira",
	}
	return Result{Outcome: model.StatusSuccess, Output: mem, MemoryWrites: mem,
		Logs: []string{fmt.Sprintf("jira: fetched %s — %s", issue.Key, issue.Summary)}}
}

func jiraComment(n model.Node, ex func(string) string, project string) Result {
	base, auth, ok := jiraCreds(n, project, ex)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "missing Jira config"},
			Logs:   []string{"jira_comment: missing base_url / email / token"}}
	}
	key := firstNonEmpty(ex("issue_key"), ex("key"))
	body := ex("body")
	if key == "" || strings.Contains(key, "${") || body == "" {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "issue_key and body are required"},
			Logs:   []string{"jira_comment: issue_key and body required"}}
	}
	endpoint := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", base, url.PathEscape(key))
	raw, status, err := postJSON(endpoint, jiraHeaders(auth), map[string]any{"body": adfDoc(body)})
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()},
			Logs: []string{fmt.Sprintf("jira_comment: POST -> error: %v", err)}}
	}
	if status < 200 || status >= 300 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": apiErrSnippet(raw)},
			Logs:   []string{fmt.Sprintf("jira_comment: %s -> %d: %s", key, status, apiErrSnippet(raw))}}
	}
	var c struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &c)
	return Result{Outcome: model.StatusSuccess,
		Output: map[string]any{"issue_key": key, "comment_id": c.ID, "url": fmt.Sprintf("%s/browse/%s", base, key)},
		Logs:   []string{fmt.Sprintf("jira_comment: commented on %s", key)}}
}

type jiraIssue struct {
	Key, Summary, Description, Status string
}

// firstJiraIssue parses either a /search response (issues[]) or a single /issue
// response and returns the first issue with text extracted from the ADF body.
func firstJiraIssue(raw []byte) (jiraIssue, bool) {
	type fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
	}
	type issue struct {
		Key    string `json:"key"`
		Fields fields `json:"fields"`
	}
	var single issue
	if err := json.Unmarshal(raw, &single); err == nil && single.Key != "" {
		return jiraIssue{single.Key, single.Fields.Summary, adfToText(single.Fields.Description), single.Fields.Status.Name}, true
	}
	var search struct {
		Issues []issue `json:"issues"`
	}
	if err := json.Unmarshal(raw, &search); err == nil && len(search.Issues) > 0 {
		i := search.Issues[0]
		return jiraIssue{i.Key, i.Fields.Summary, adfToText(i.Fields.Description), i.Fields.Status.Name}, true
	}
	return jiraIssue{}, false
}

// adfToText flattens an Atlassian Document Format body to plain text by collecting
// every "text" leaf, inserting line breaks for block boundaries.
func adfToText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if txt, ok := t["text"].(string); ok {
				b.WriteString(txt)
			}
			if typ, _ := t["type"].(string); typ == "paragraph" || typ == "heading" {
				b.WriteString("\n")
			}
			if content, ok := t["content"].([]any); ok {
				for _, c := range content {
					walk(c)
				}
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(node)
	return strings.TrimSpace(b.String())
}

// adfDoc wraps plain text (one paragraph per line) into a minimal ADF document,
// the shape Jira Cloud's comment API expects.
func adfDoc(text string) map[string]any {
	var paras []any
	for _, line := range strings.Split(text, "\n") {
		content := []any{}
		if strings.TrimSpace(line) != "" {
			content = []any{map[string]any{"type": "text", "text": line}}
		}
		paras = append(paras, map[string]any{"type": "paragraph", "content": content})
	}
	if len(paras) == 0 {
		paras = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	return map[string]any{"type": "doc", "version": 1, "content": paras}
}
