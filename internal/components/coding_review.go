// Diff review before push.
//
// When a coding_apply node has `push: false`, it does not push. It records a
// change set — the exact new content of every changed file, on top of a recorded
// base commit — plus a unified diff, in run memory. An approval gate shows that
// diff (its `diff` field, e.g. ${memory._change_diff}); after approval, a
// coding_push node commits exactly the reviewed change set through the GitHub Git
// Data API and moves the branch. Both coding engines produce the same change set,
// so the review looks the same either way.
//
// Memory keys starting with "_" are internal (hidden from the Memory tab).
package components

import (
	"encoding/json"
	"fmt"
	"strings"

	"conduit/internal/model"
)

const (
	memChangeSet  = "_change_set"
	memChangeDiff = "_change_diff"
	memChangeStat = "_change_stat"

	maxChangeSetBytes = 4 << 20   // total file content kept for review
	maxDiffBytes      = 300 << 10 // diff shown to the reviewer
)

type changeSet struct {
	Repo    string     `json:"repo"`
	Base    string     `json:"base"`     // branch the change starts from
	BaseSHA string     `json:"base_sha"` // exact commit it was made against
	Branch  string     `json:"branch"`   // branch it will be pushed to
	Message string     `json:"message"`  // commit message
	Files   []fileEdit `json:"files"`
	Plan    string     `json:"plan,omitempty"`
}

type fileEdit struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // create | modify | delete
	Content string `json:"content,omitempty"`
}

// pushAfterReview reports whether a coding_apply node should stop before pushing
// (field push=false) and leave the change set for review.
func pushAfterReview(n model.Node) bool {
	switch strings.ToLower(strings.TrimSpace(n.Field("push"))) {
	case "false", "no", "0", "after review", "after_review", "review":
		return true
	}
	return false
}

// prepared builds the Result of a prepare-only apply: the change set and diff go
// to memory for the review gate and the push step; nothing has left the server.
func prepared(cs changeSet, old map[string]string, out map[string]any, logs []string) Result {
	var diff strings.Builder
	type fileStat struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Added   int    `json:"added"`
		Removed int    `json:"removed"`
	}
	var stats []fileStat
	var adds, dels, size int
	for _, f := range cs.Files {
		size += len(f.Content)
		d, a, r := unifiedDiff(f.Path, old[f.Path], f.Content, f.Action)
		diff.WriteString(d)
		stats = append(stats, fileStat{f.Path, f.Action, a, r})
		adds += a
		dels += r
	}
	if size > maxChangeSetBytes {
		return failCode(fmt.Sprintf("the change is too large to review (%d KB of file content, limit %d KB) — split the ticket, or remove the Review Diff step", size>>10, maxChangeSetBytes>>10))
	}
	d := diff.String()
	if len(d) > maxDiffBytes {
		d = d[:maxDiffBytes] + "\n… (diff truncated for display; the full change is pushed)\n"
	}
	stat := fmt.Sprintf("%d file(s) changed, +%d −%d", len(cs.Files), adds, dels)
	raw, _ := json.Marshal(cs)
	if out == nil {
		out = map[string]any{}
	}
	out["prepared"] = true
	out["branch"] = cs.Branch
	out["base"] = cs.Base
	out["diff_stat"] = stat
	out["files"] = stats
	out["diff"] = d
	return Result{Outcome: model.StatusSuccess, Output: out,
		MemoryWrites: map[string]any{memChangeSet: string(raw), memChangeDiff: d, memChangeStat: stat,
			"change_plan": firstNonEmpty(cs.Plan, stat)},
		Logs: append(logs, "coding: prepared for review — "+stat+" (not pushed yet)")}
}

// execCodingPush (coding_push) commits the reviewed change set exactly as
// approved, on top of the base commit it was made against, and moves the branch.
func execCodingPush(n model.Node, run *model.Run) Result {
	cfg, ex, fail := resolveCoding(n, run, true)
	if fail != nil {
		return *fail
	}
	raw := firstNonEmpty(ex("change_set"), fmt.Sprint(run.Memory[memChangeSet]))
	var cs changeSet
	if err := json.Unmarshal([]byte(raw), &cs); err != nil || len(cs.Files) == 0 {
		return failCode("no reviewed change set to push (an Apply Changes step with push: false must run first)")
	}
	if want := cfg.owner + "/" + cfg.repo; !strings.EqualFold(cs.Repo, want) {
		return failCode(fmt.Sprintf("the reviewed change is for %s, not %s", cs.Repo, want))
	}
	treeSHA, msg := cfg.gh.commitTree(cs.BaseSHA)
	if msg != "" {
		return failCode(msg)
	}
	files := make([]fileChange, len(cs.Files))
	for i, f := range cs.Files {
		files[i] = fileChange{Path: f.Path, Content: f.Content, Delete: f.Action == "delete"}
	}
	newTree, msg := cfg.gh.makeTree(treeSHA, files)
	if msg != "" {
		return failCode(msg)
	}
	sha, msg := cfg.gh.makeCommit(firstNonEmpty(cs.Message, "automated change"), newTree, cs.BaseSHA)
	if msg != "" {
		return failCode(msg)
	}
	if msg := cfg.gh.upsertBranch(cs.Branch, sha); msg != "" {
		return failCode(msg)
	}
	cfg.base = cs.Base
	res := cfg.applyResult(cs.Branch, sha, firstNonEmpty(cs.Plan, fmt.Sprint(run.Memory[memChangeStat])), files)
	res.Output["diff_stat"] = run.Memory[memChangeStat]
	res.Output["reviewed"] = true
	return res
}

// commitTree returns the tree of a commit.
func (g ghClient) commitTree(sha string) (string, string) {
	raw, status, err := getWithHeader(g.url("git/commits/"+sha), g.headers)
	if err != nil || status != 200 {
		return "", "could not read the base commit " + sha + ": " + ghErr(raw, status, err)
	}
	var c struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.Tree.SHA, ""
}

// --- unified diff ------------------------------------------------------------------

const diffContext = 3

// maxDiffCells bounds the LCS table; bigger files are shown as a full replacement.
const maxDiffCells = 4_000_000

// unifiedDiff renders a git-style unified diff for one file and returns it with
// the number of added and removed lines.
func unifiedDiff(path, old, cur, action string) (string, int, int) {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	from, to := "a/"+path, "b/"+path
	switch action {
	case "create":
		b.WriteString("new file mode 100644\n")
		from, old = "/dev/null", ""
	case "delete":
		b.WriteString("deleted file mode 100644\n")
		to, cur = "/dev/null", ""
	}
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", from, to)
	a, c := splitLines(old), splitLines(cur)
	ops := diffOps(a, c)
	adds, dels := 0, 0
	for _, o := range ops {
		switch o.kind {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	writeHunks(&b, ops)
	return b.String(), adds, dels
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

type diffOp struct {
	kind       byte // ' ', '-', '+'
	text       string
	aLine, bLn int // 1-based positions (of the line in a / in b)
}

// diffOps computes a line diff with a longest-common-subsequence table, after
// trimming the common prefix and suffix (so typical edits stay cheap).
func diffOps(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	var ops []diffOp
	for i := 0; i < pre; i++ {
		ops = append(ops, diffOp{' ', a[i], i + 1, i + 1})
	}
	ai, bi := pre, pre
	add := func(kind byte, text string) {
		switch kind {
		case ' ':
			ai++
			bi++
			ops = append(ops, diffOp{' ', text, ai, bi})
		case '-':
			ai++
			ops = append(ops, diffOp{'-', text, ai, bi})
		case '+':
			bi++
			ops = append(ops, diffOp{'+', text, ai, bi})
		}
	}
	if len(ma)*len(mb) > maxDiffCells {
		for _, l := range ma {
			add('-', l)
		}
		for _, l := range mb {
			add('+', l)
		}
	} else {
		n, m := len(ma), len(mb)
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else if lcs[i+1][j] >= lcs[i][j+1] {
					lcs[i][j] = lcs[i+1][j]
				} else {
					lcs[i][j] = lcs[i][j+1]
				}
			}
		}
		i, j := 0, 0
		for i < n || j < m {
			switch {
			case i < n && j < m && ma[i] == mb[j]:
				add(' ', ma[i])
				i++
				j++
			case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
				add('-', ma[i]) // removals before additions, like git
				i++
			default:
				add('+', mb[j])
				j++
			}
		}
	}
	for k := len(a) - suf; k < len(a); k++ {
		add(' ', a[k])
	}
	return ops
}

// writeHunks groups changes with diffContext lines of context into @@ hunks.
func writeHunks(b *strings.Builder, ops []diffOp) {
	n := len(ops)
	for i := 0; i < n; {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(0, i-diffContext)
		end := i
		for end < n {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < n && ops[run].kind == ' ' {
				run++
			}
			if run == n || run-end > 2*diffContext {
				end = min(n, end+diffContext)
				break
			}
			end = run
		}
		aStart, bStart, aCount, bCount := 0, 0, 0, 0
		for k := start; k < end; k++ {
			o := ops[k]
			if o.kind != '+' {
				aCount++
				if aStart == 0 {
					aStart = o.aLine
				}
			}
			if o.kind != '-' {
				bCount++
				if bStart == 0 {
					bStart = o.bLn
				}
			}
		}
		if aStart == 0 {
			aStart = ops[start].aLine
		}
		if bStart == 0 {
			bStart = ops[start].bLn
		}
		fmt.Fprintf(b, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for k := start; k < end; k++ {
			b.WriteByte(ops[k].kind)
			b.WriteString(ops[k].text)
			b.WriteByte('\n')
		}
		i = end
	}
}
