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

// codingCfg is the resolved configuration shared by the coding components.
type codingCfg struct {
	owner, repo, base                   string
	provider, apiKey, baseURL, model    string
	maxFiles, maxTokens                 int
	ticket, ticketSummary, branchPrefix string
	gh                                  ghClient
	hasToken                            bool
}

// resolveCoding resolves the common config for a coding node. needToken fails the
// node when a GitHub token is required (apply/all-in-one) but absent.
func resolveCoding(n model.Node, run *model.Run, needToken bool) (codingCfg, func(string) string, *Result) {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }

	owner, repo := ex("owner"), ex("repo")
	if owner == "" || repo == "" || strings.Contains(owner, "${") || strings.Contains(repo, "${") {
		r := failCode("owner and repo are required")
		return codingCfg{}, ex, &r
	}
	cfg := codingCfg{
		owner: owner, repo: repo,
		base:          firstNonEmpty(ex("base"), "main"),
		provider:      strings.ToLower(firstNonEmpty(ex("provider"), "anthropic")),
		apiKey:        secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("key"))),
		baseURL:       ex("base_url"),
		model:         ex("model"),
		maxFiles:      10,
		ticket:        firstNonEmpty(ex("ticket"), "change"),
		ticketSummary: ex("ticket_summary"),
		branchPrefix:  firstNonEmpty(ex("branch_prefix"), "conduit/"),
	}
	if v, err := strconv.Atoi(n.Field("max_files")); err == nil && v > 0 {
		cfg.maxFiles = v
	}
	// Implementing full file contents can be large; a low cap truncates the JSON
	// reply mid-array ("unexpected end of JSON input"). Default generously and let
	// a node raise it further via max_tokens.
	cfg.maxTokens = 8192
	if v, err := strconv.Atoi(ex("max_tokens")); err == nil && v > 0 {
		cfg.maxTokens = v
	}
	if token, ok := githubToken(n, project); ok {
		cfg.gh = ghClient{base: githubBase(n), owner: owner, repo: repo, headers: githubHeaders(token)}
		cfg.hasToken = true
	} else if needToken {
		r := Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no github token", "hint": "store a secret named github_token with contents:write access"},
			Logs:   []string{"coding_agent: no github token"}}
		return cfg, ex, &r
	}
	return cfg, ex, nil
}

// planChanges asks the model which files to create vs. modify, with details.
func (c codingCfg) planChanges(task, notes string) ([]changeItem, *Result) {
	prompt := fmt.Sprintf(`You are a senior software engineer in the repo %s/%s.
Produce a CHANGE PLAN for the work below: the exact set of files to touch.
Respond with ONLY a JSON array (no prose, no fences) of objects:
[{"path":"relative/path.ext","action":"create"|"modify","details":"what changes and why"}]
List at most %d files. Use "create" only for files that do not exist yet.

WORK:
%s

NOTES:
%s`, c.owner, c.repo, c.maxFiles, task, notes)

	text, status, err := callLLM(c.provider, c.baseURL, c.apiKey, c.model, prompt, c.maxTokens)
	if err != nil || status < 200 || status >= 300 {
		r := llmFail("plan", err, status)
		return nil, &r
	}
	items, perr := parseChangePlan(text, c.maxFiles)
	if perr != "" {
		r := failCode(perr)
		return nil, &r
	}
	return items, nil
}

// implementChanges pulls current contents for "modify" files (downgrading any
// that don't exist on base to "create"), then asks the model for full contents.
func (c codingCfg) implementChanges(items []changeItem, task, feedback string) ([]fileChange, *Result) {
	var ctxBuf strings.Builder
	for i := range items {
		if !strings.EqualFold(items[i].Action, "modify") {
			continue
		}
		content, exists := c.gh.fileContent(items[i].Path, c.base)
		if !exists {
			items[i].Action = "create"
			continue
		}
		if len(content) > 12000 {
			content = content[:12000] + "\n… (truncated)"
		}
		fmt.Fprintf(&ctxBuf, "\n--- current content of %s ---\n%s\n", items[i].Path, content)
	}
	prompt := fmt.Sprintf(`You are a senior software engineer in the repo %s/%s.
Implement the change plan below. For each file, return its COMPLETE new content —
for a "modify" file, edit the CURRENT CONTENT shown (do not drop existing code).
Respond with ONLY a JSON array (no prose, no fences) of objects:
[{"path":"relative/path.ext","content":"<full file content>"}]

CHANGE PLAN:
%s

CURRENT FILE CONTENTS:%s

ORIGINAL TASK:
%s%s`, c.owner, c.repo, formatChangePlan(items), firstNonEmpty(ctxBuf.String(), " (all files are new)"), task, feedbackBlock(feedback))

	text, status, err := callLLM(c.provider, c.baseURL, c.apiKey, c.model, prompt, c.maxTokens)
	if err != nil || status < 200 || status >= 300 {
		r := llmFail("implement", err, status)
		return nil, &r
	}
	files, perr := parseFileChanges(text, c.maxFiles)
	if perr != "" {
		r := failCode(perr)
		return nil, &r
	}
	return files, nil
}

// commitBranch commits the files to a fresh branch via the Git Data API.
func (c codingCfg) commitBranch(files []fileChange) (branch, commitSHA string, errRes *Result) {
	baseSHA, treeSHA, cerr := c.gh.baseCommit(c.base)
	if cerr != "" {
		r := failCode(cerr)
		return "", "", &r
	}
	branch = branchName(c.branchPrefix, c.ticket)
	newTree, cerr := c.gh.makeTree(treeSHA, files)
	if cerr != "" {
		r := failCode(cerr)
		return "", "", &r
	}
	msg := fmt.Sprintf("%s: %s", c.ticket, firstLine(firstNonEmpty(c.ticketSummary, "automated change")))
	commitSHA, cerr = c.gh.makeCommit(msg, newTree, baseSHA)
	if cerr != "" {
		r := failCode(cerr)
		return "", "", &r
	}
	if cerr := c.gh.upsertBranch(branch, commitSHA); cerr != "" {
		r := failCode(cerr)
		return "", "", &r
	}
	return branch, commitSHA, nil
}

// applyResult builds the common success Result for a committed branch.
func (c codingCfg) applyResult(branch, commitSHA, planText string, files []fileChange) Result {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	out := map[string]any{
		"work_branch":   branch,
		"base":          c.base,
		"commit":        commitSHA,
		"changed_files": paths,
		"change_plan":   planText,
		"repo":          c.owner + "/" + c.repo,
	}
	return Result{Outcome: model.StatusSuccess, Output: out, MemoryWrites: map[string]any{
		"work_branch":  branch,
		"commit_sha":   commitSHA,
		"changed_list": strings.Join(paths, ", "),
		"change_plan":  planText,
	}, Logs: []string{
		"coding: change plan —\n" + planText,
		fmt.Sprintf("coding: pushed %d file(s) to %s (%s)", len(files), branch, commitSHA[:min(7, len(commitSHA))]),
	}}
}

// execCodingPlan (coding_plan) runs ONLY the planning phase: it produces the
// file-level change plan and writes it to memory (change_plan for display,
// change_plan_json for a downstream coding_apply node) without touching the repo.
// Put a Human Approval after it to approve the exact file list before any branch.
func execCodingPlan(n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveCoding(n, run, false)
	if fail != nil {
		return *fail
	}
	task := ex("plan")
	if strings.TrimSpace(task) == "" || strings.Contains(task, "${memory.") {
		return failCode("no plan to work from (set plan, e.g. ${memory.plan})")
	}
	items, r := cfg.planChanges(task, ex("implementation"))
	if r != nil {
		return *r
	}
	formatted := formatChangePlan(items)
	rawJSON, _ := json.Marshal(items)
	return Result{Outcome: model.StatusSuccess,
		Output: map[string]any{"change_plan": formatted, "files": len(items), "repo": cfg.owner + "/" + cfg.repo},
		MemoryWrites: map[string]any{
			"change_plan":      formatted,
			"change_plan_json": string(rawJSON),
		},
		Logs: []string{"coding_plan: proposed changes —\n" + formatted}}
}

// execCodingApply (coding_apply) runs the apply phase on an already-approved plan:
// it reads the structured change plan from memory (change_plan_json), pulls current
// contents for modified files, has the model produce full file contents, and
// commits them to a new branch. Config `changes` should be ${memory.change_plan_json}.
func execCodingApply(n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveCoding(n, run, true)
	if fail != nil {
		return *fail
	}
	changesJSON := firstNonEmpty(ex("changes"), ex("change_plan_json"))
	if strings.TrimSpace(changesJSON) == "" || strings.Contains(changesJSON, "${memory.") {
		return failCode("no approved change plan (set changes to ${memory.change_plan_json} from a coding_plan node)")
	}
	items, perr := parseChangePlan(changesJSON, cfg.maxFiles)
	if perr != "" {
		return failCode("could not read approved change plan: " + perr)
	}
	// On a recovery-loop retry, prior CI feedback (e.g. ${memory.checks_summary})
	// is passed in via the `feedback` field so the re-run fixes what failed.
	files, r := cfg.implementChanges(items, ex("plan"), ex("feedback"))
	if r != nil {
		return *r
	}
	branch, commitSHA, r := cfg.commitBranch(files)
	if r != nil {
		return *r
	}
	return cfg.applyResult(branch, commitSHA, formatChangePlan(items), files)
}

// execCodingAgent (coding_agent) is the all-in-one: plan -> pull contents ->
// implement -> commit, with no intermediate approval. Config: owner, repo, base
// (default main), token (${secret:github_token}), provider/model/api_key (LLM),
// plan, implementation, ticket, branch_prefix (default conduit/), max_files.
func execCodingAgent(n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveCoding(n, run, true)
	if fail != nil {
		return *fail
	}
	task := ex("plan")
	if strings.TrimSpace(task) == "" || strings.Contains(task, "${memory.") {
		return failCode("no plan to implement (set plan, e.g. ${memory.plan})")
	}
	items, r := cfg.planChanges(task, ex("implementation"))
	if r != nil {
		return *r
	}
	files, r := cfg.implementChanges(items, task, ex("feedback"))
	if r != nil {
		return *r
	}
	branch, commitSHA, r := cfg.commitBranch(files)
	if r != nil {
		return *r
	}
	return cfg.applyResult(branch, commitSHA, formatChangePlan(items), files)
}

// feedbackBlock renders prior failure context (e.g. failing CI checks from an
// earlier attempt) into the implement prompt, or "" when there is none.
func feedbackBlock(feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" || strings.Contains(feedback, "${memory.") {
		return ""
	}
	return "\n\nA PREVIOUS ATTEMPT FAILED VERIFICATION — fix these and keep the rest working:\n" + feedback
}

// truncationHint annotates a JSON parse error that looks like a cut-off response
// (the common cause when a model hits its token cap mid-array) with how to fix it.
func truncationHint(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "unexpected end of JSON") || strings.Contains(msg, "unexpected EOF") {
		msg += " (the model's response looks truncated — raise max_tokens on this node, or lower max_files)"
	}
	return msg
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
		return nil, "could not parse change plan: " + truncationHint(err)
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
		return nil, "could not parse file changes: " + truncationHint(err)
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
