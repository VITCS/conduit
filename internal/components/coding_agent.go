// coding_agent turns an approved plan into an actual branch on GitHub: it asks a
// model for the concrete file changes, then commits them to a new branch via the
// GitHub Git Data API (create blobs -> tree -> commit -> ref). No local checkout
// is needed — the branch is built entirely through the API — and the resulting
// branch name is written to memory as work_branch, so a downstream github_pr node
// opens a real pull request from it.
package components

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"strconv"
	"strings"

	"conduit/internal/model"
)

var branchSanitize = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

type fileChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// execCodingAgent implements the plan on a new branch and returns success with
// work_branch set in memory. Config: owner, repo, base (default main), token
// (${secret:github_token}), provider/model/api_key (LLM), plan, implementation,
// ticket (for branch + commit message), branch_prefix (default conduit/),
// max_files (default 10), api_base (enterprise).
func execCodingAgent(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }

	owner, repo := ex("owner"), ex("repo")
	base := firstNonEmpty(ex("base"), "main")
	if owner == "" || repo == "" || strings.Contains(owner, "${") || strings.Contains(repo, "${") {
		return failCode("owner and repo are required")
	}
	token, ok := githubToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "store a secret named github_token with contents:write access"},
			Logs:   []string{"coding_agent: no github token"}}
	}

	plan := ex("plan")
	if strings.TrimSpace(plan) == "" || strings.Contains(plan, "${memory.") {
		return failCode("no plan to implement (set plan, e.g. ${memory.plan})")
	}

	provider := strings.ToLower(firstNonEmpty(ex("provider"), "anthropic"))
	apiKey := secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("key")))
	baseURL := ex("base_url")
	modelName := ex("model")
	maxFiles := 10
	if v, err := strconv.Atoi(n.Field("max_files")); err == nil && v > 0 {
		maxFiles = v
	}
	gh := ghClient{base: githubBase(n), owner: owner, repo: repo, headers: githubHeaders(token)}

	// 1) PLAN: ask the model which files to create vs. modify, with details.
	planPrompt := fmt.Sprintf(`You are a senior software engineer in the repo %s/%s.
Produce a CHANGE PLAN for the work below: the exact set of files to touch.
Respond with ONLY a JSON array (no prose, no fences) of objects:
[{"path":"relative/path.ext","action":"create"|"modify","details":"what changes and why"}]
List at most %d files. Use "create" only for files that do not exist yet.

WORK:
%s

NOTES:
%s`, owner, repo, maxFiles, plan, ex("implementation"))

	planText, status, err := callLLM(provider, baseURL, apiKey, modelName, planPrompt, 2000)
	if err != nil || status < 200 || status >= 300 {
		return llmFail("plan", err, status)
	}
	changes, perr := parseChangePlan(planText, maxFiles)
	if perr != "" {
		return failCode(perr)
	}
	planText = formatChangePlan(changes)

	// 2) CONTEXT: pull current contents of every file marked "modify".
	var ctxBuf strings.Builder
	for i := range changes {
		if !strings.EqualFold(changes[i].Action, "modify") {
			continue
		}
		content, exists := gh.fileContent(changes[i].Path, base)
		if !exists {
			changes[i].Action = "create" // listed as modify but absent on base
			continue
		}
		if len(content) > 12000 {
			content = content[:12000] + "\n… (truncated)"
		}
		fmt.Fprintf(&ctxBuf, "\n--- current content of %s ---\n%s\n", changes[i].Path, content)
	}

	// 3) IMPLEMENT: produce full file contents, given the plan + current contents.
	implPrompt := fmt.Sprintf(`You are a senior software engineer in the repo %s/%s.
Implement the change plan below. For each file, return its COMPLETE new content —
for a "modify" file, edit the CURRENT CONTENT shown (do not drop existing code).
Respond with ONLY a JSON array (no prose, no fences) of objects:
[{"path":"relative/path.ext","content":"<full file content>"}]

CHANGE PLAN:
%s

CURRENT FILE CONTENTS:%s

ORIGINAL TASK:
%s`, owner, repo, planText, firstNonEmpty(ctxBuf.String(), " (all files are new)"), plan)

	implText, status, err := callLLM(provider, baseURL, apiKey, modelName, implPrompt, 4000)
	if err != nil || status < 200 || status >= 300 {
		return llmFail("implement", err, status)
	}
	files, perr := parseFileChanges(implText, maxFiles)
	if perr != "" {
		return failCode(perr)
	}

	// 4) Commit the files to a fresh branch via the Git Data API.
	baseSHA, treeSHA, cerr := gh.baseCommit(base)
	if cerr != "" {
		return failCode(cerr)
	}
	branch := branchName(firstNonEmpty(ex("branch_prefix"), "conduit/"), firstNonEmpty(ex("ticket"), "change"))
	newTree, cerr := gh.makeTree(treeSHA, files)
	if cerr != "" {
		return failCode(cerr)
	}
	msg := fmt.Sprintf("%s: %s", firstNonEmpty(ex("ticket"), "change"), firstLine(firstNonEmpty(ex("ticket_summary"), "automated change")))
	commitSHA, cerr := gh.makeCommit(msg, newTree, baseSHA)
	if cerr != "" {
		return failCode(cerr)
	}
	if cerr := gh.upsertBranch(branch, commitSHA); cerr != "" {
		return failCode(cerr)
	}

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	out := map[string]any{
		"work_branch":   branch,
		"base":          base,
		"commit":        commitSHA,
		"changed_files": paths,
		"change_plan":   planText,
		"repo":          owner + "/" + repo,
	}
	return Result{Outcome: model.StatusSuccess, Output: out, MemoryWrites: map[string]any{
		"work_branch":  branch,
		"commit_sha":   commitSHA,
		"changed_list": strings.Join(paths, ", "),
		"change_plan":  planText,
	}, Logs: []string{
		"coding_agent: change plan —\n" + planText,
		fmt.Sprintf("coding_agent: pushed %d file(s) to %s (%s)", len(files), branch, commitSHA[:min(7, len(commitSHA))]),
	}}
}

func failCode(msg string) Result {
	return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": msg}, Logs: []string{"coding_agent: " + msg}}
}

func llmFail(phase string, err error, status int) Result {
	msg := "request failed"
	if err != nil {
		msg = err.Error()
	}
	return Result{Outcome: model.StatusFailed,
		Output: map[string]any{"error": msg, "status_code": status, "phase": phase},
		Logs:   []string{fmt.Sprintf("coding_agent: %s LLM failed: %s (status %d)", phase, msg, status)}}
}

type changeItem struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Details string `json:"details"`
}

// parseChangePlan extracts the JSON array of {path,action,details} describing the
// files to create/modify, tolerating surrounding prose or ```json fences.
func parseChangePlan(text string, maxFiles int) ([]changeItem, string) {
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start < 0 || end <= start {
		return nil, "model did not return a JSON change plan"
	}
	var items []changeItem
	if err := json.Unmarshal([]byte(text[start:end+1]), &items); err != nil {
		return nil, "could not parse change plan: " + err.Error()
	}
	cleaned := items[:0]
	for _, it := range items {
		if strings.TrimSpace(it.Path) == "" || strings.Contains(it.Path, "..") || strings.HasPrefix(it.Path, "/") {
			continue
		}
		if !strings.EqualFold(it.Action, "create") && !strings.EqualFold(it.Action, "modify") {
			it.Action = "modify"
		}
		cleaned = append(cleaned, it)
	}
	if len(cleaned) == 0 {
		return nil, "change plan listed no valid files"
	}
	if len(cleaned) > maxFiles {
		cleaned = cleaned[:maxFiles]
	}
	return cleaned, ""
}

// formatChangePlan renders the plan as a readable checklist for memory/logs/PR.
func formatChangePlan(items []changeItem) string {
	var b strings.Builder
	for _, it := range items {
		verb := "modify"
		if strings.EqualFold(it.Action, "create") {
			verb = "create"
		}
		fmt.Fprintf(&b, "• [%s] %s", verb, it.Path)
		if d := strings.TrimSpace(it.Details); d != "" {
			fmt.Fprintf(&b, " — %s", d)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func branchName(prefix, ticket string) string {
	b := prefix + branchSanitize.ReplaceAllString(strings.TrimSpace(ticket), "-")
	return strings.Trim(b, "-/")
}

// parseFileChanges extracts the JSON array of {path,content} from a model reply,
// tolerating surrounding prose or ```json fences.
func parseFileChanges(text string, maxFiles int) ([]fileChange, string) {
	start := strings.IndexByte(text, '[')
	end := strings.LastIndexByte(text, ']')
	if start < 0 || end <= start {
		return nil, "model did not return a JSON array of file changes"
	}
	var files []fileChange
	if err := json.Unmarshal([]byte(text[start:end+1]), &files); err != nil {
		return nil, "could not parse file changes: " + err.Error()
	}
	cleaned := files[:0]
	for _, f := range files {
		if strings.TrimSpace(f.Path) == "" || strings.Contains(f.Path, "..") || strings.HasPrefix(f.Path, "/") {
			continue
		}
		cleaned = append(cleaned, f)
	}
	if len(cleaned) == 0 {
		return nil, "model returned no valid file changes"
	}
	if len(cleaned) > maxFiles {
		cleaned = cleaned[:maxFiles]
	}
	return cleaned, ""
}

// ghClient is a thin Git Data API helper scoped to one repo.
type ghClient struct {
	base, owner, repo string
	headers           map[string]string
}

func (g ghClient) url(path string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", g.base, g.owner, g.repo, path)
}

// fileContent fetches a file's current content on a ref via the Contents API,
// base64-decoded. exists is false when the file is absent (404) on that ref.
func (g ghClient) fileContent(path, ref string) (content string, exists bool) {
	url := g.url("contents/"+path) + "?ref=" + urlpkg.QueryEscape(ref)
	raw, status, err := getWithHeader(url, g.headers)
	if err != nil || status == 404 {
		return "", false
	}
	if status < 200 || status >= 300 {
		return "", false
	}
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", false
	}
	if body.Encoding == "base64" {
		dec, derr := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.Content, "\n", ""))
		if derr != nil {
			return "", false
		}
		return string(dec), true
	}
	return body.Content, true
}

func (g ghClient) baseCommit(branch string) (commitSHA, treeSHA, errMsg string) {
	raw, status, err := getWithHeader(g.url("git/ref/heads/"+branch), g.headers)
	if err != nil || status != 200 {
		return "", "", fmt.Sprintf("base branch %q not found: %s", branch, ghErr(raw, status, err))
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	_ = json.Unmarshal(raw, &ref)
	raw, status, err = getWithHeader(g.url("git/commits/"+ref.Object.SHA), g.headers)
	if err != nil || status != 200 {
		return "", "", "could not read base commit: " + ghErr(raw, status, err)
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	_ = json.Unmarshal(raw, &commit)
	return ref.Object.SHA, commit.Tree.SHA, ""
}

func (g ghClient) makeTree(baseTree string, files []fileChange) (string, string) {
	entries := make([]map[string]any, 0, len(files))
	for _, f := range files {
		blobRaw, status, err := postJSON(g.url("git/blobs"), g.headers, map[string]any{"content": f.Content, "encoding": "utf-8"})
		if err != nil || status < 200 || status >= 300 {
			return "", "failed to create blob for " + f.Path + ": " + ghErr(blobRaw, status, err)
		}
		var blob struct {
			SHA string `json:"sha"`
		}
		_ = json.Unmarshal(blobRaw, &blob)
		entries = append(entries, map[string]any{"path": f.Path, "mode": "100644", "type": "blob", "sha": blob.SHA})
	}
	raw, status, err := postJSON(g.url("git/trees"), g.headers, map[string]any{"base_tree": baseTree, "tree": entries})
	if err != nil || status < 200 || status >= 300 {
		return "", "failed to create tree: " + ghErr(raw, status, err)
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	_ = json.Unmarshal(raw, &tree)
	return tree.SHA, ""
}

func (g ghClient) makeCommit(message, tree, parent string) (string, string) {
	raw, status, err := postJSON(g.url("git/commits"), g.headers, map[string]any{
		"message": message, "tree": tree, "parents": []string{parent},
	})
	if err != nil || status < 200 || status >= 300 {
		return "", "failed to create commit: " + ghErr(raw, status, err)
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	_ = json.Unmarshal(raw, &commit)
	return commit.SHA, ""
}

func (g ghClient) upsertBranch(branch, commitSHA string) string {
	raw, status, err := postJSON(g.url("git/refs"), g.headers, map[string]any{"ref": "refs/heads/" + branch, "sha": commitSHA})
	if err == nil && status >= 200 && status < 300 {
		return ""
	}
	// Branch already exists -> fast-forward (force) it to the new commit.
	if status == 422 {
		raw, status, err = sendJSON(http.MethodPatch, g.url("git/refs/heads/"+branch), g.headers, map[string]any{"sha": commitSHA, "force": true})
		if err == nil && status >= 200 && status < 300 {
			return ""
		}
	}
	return "failed to create/update branch " + branch + ": " + ghErr(raw, status, err)
}

func ghErr(raw []byte, status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return githubError(raw, status)
}

// sendJSON performs an arbitrary-method JSON request via the SSRF-guarded client.
func sendJSON(method, url string, headers map[string]string, body any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, url, strings.NewReader(string(b)))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
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
