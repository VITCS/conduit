package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"conduit/internal/model"
)

// workflowStore is the subset of store.Store that SyncBuiltins needs.
type workflowStore interface {
	GetWorkflow(id string) (model.Workflow, bool, error)
	PutWorkflow(w model.Workflow) error
}

// builtin is a shipped workflow plus a test for recognizing an out-of-date copy
// that predates content-hash stamping (SeedHash == ""), so it can be refreshed
// exactly once without touching a workflow the user has customized.
type builtin struct {
	wf model.Workflow
	// legacy reports whether a stored copy with no SeedHash is an older built-in
	// generation (not a user creation) and should be replaced by wf. It only ever
	// runs on the matching built-in ID, and only when SeedHash is empty.
	legacy func(got model.Workflow) bool
}

// Builtins returns every workflow the server ships, each paired with a detector
// for its superseded, pre-versioning shape.
func Builtins() []builtin {
	return []builtin{
		{wf: Workflow()}, // Preview Environments (default project)
		{wf: GitHubWorkflow()},
		{
			wf: FactoryWorkflow(),
			// The legacy AI Factory ("Issue → Reviewed PR" and its variants) was a
			// pure simulation: it drafted with `agent` nodes and "opened" a PR with a
			// generic `http` node. Every such generation lacks a coding_apply node;
			// the real pipeline always has one. So "no coding_apply" uniquely marks a
			// stale demo, independent of which exact legacy revision was seeded.
			legacy: func(got model.Workflow) bool { return !hasNodeType(got, "coding_apply") },
		},
	}
}

// Fingerprint is a stable content hash of a workflow's shipped shape. Rev and
// SeedHash are excluded so a stamped copy and the definition it came from hash
// alike, letting SyncBuiltins tell an untouched built-in from an edited one.
func Fingerprint(w model.Workflow) string {
	w.Rev = 0
	w.SeedHash = ""
	b, _ := json.Marshal(w)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// SyncBuiltins installs or upgrades the shipped workflows. The rules never
// overwrite a workflow the user has edited:
//
//   - missing            → install (stamped with Rev + SeedHash)
//   - identical to ours  → (re)stamp so future upgrades are detectable
//   - stamped & unedited → upgrade when we ship a newer Rev
//   - legacy & unedited  → refresh once (pre-stamping copies, matched by shape)
//   - anything else      → left untouched (user-edited, or already current)
//
// The matching Project must already exist; SyncBuiltins only touches workflows.
func SyncBuiltins(st workflowStore, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	for _, b := range Builtins() {
		want := b.wf
		want.SeedHash = Fingerprint(want)

		got, ok, err := st.GetWorkflow(want.ID)
		if err != nil {
			return err
		}
		if !ok {
			if err := st.PutWorkflow(want); err != nil {
				return err
			}
			logger.Info("seed: installed built-in workflow", "id", want.ID, "rev", want.Rev)
			continue
		}

		gotFP := Fingerprint(got)
		switch {
		case gotFP == want.SeedHash:
			// Same content. Make sure Rev/SeedHash are stamped so a future, newer
			// Rev can be recognized as a clean upgrade.
			if got.Rev != want.Rev || got.SeedHash != want.SeedHash {
				if err := st.PutWorkflow(want); err != nil {
					return err
				}
				logger.Info("seed: stamped built-in workflow", "id", want.ID, "rev", want.Rev)
			}
		case got.SeedHash != "" && gotFP == got.SeedHash && want.Rev > got.Rev:
			// Previously stamped and untouched since: a provably clean upgrade.
			if err := st.PutWorkflow(want); err != nil {
				return err
			}
			logger.Info("seed: upgraded built-in workflow", "id", want.ID, "from_rev", got.Rev, "to_rev", want.Rev)
		case got.SeedHash == "" && b.legacy != nil && b.legacy(got):
			// A pre-versioning built-in in its original shipped shape (user never
			// edited it) → refresh to the current definition, now stamped.
			if err := st.PutWorkflow(want); err != nil {
				return err
			}
			logger.Info("seed: refreshed legacy built-in workflow", "id", want.ID, "name", got.Name, "to_rev", want.Rev)
		default:
			// User-edited (or newer than we ship) — never overwrite.
			logger.Debug("seed: left built-in workflow as-is", "id", want.ID)
		}
	}
	return nil
}

func hasNodeType(w model.Workflow, t string) bool {
	for _, n := range w.Nodes {
		if n.Type == t {
			return true
		}
	}
	return false
}
