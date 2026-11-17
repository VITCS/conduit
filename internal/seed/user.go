package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"conduit/internal/model"
)

// Per-user workspaces.
//
// Every person who signs in with Google or GitHub gets a private copy of the
// shipped AI Factory and GitHub review workflows, in two projects they own. Their
// variables and secrets live in those projects, so each login keeps its own
// repository, model and credentials, and never sees anyone else's.
//
// IDs are derived from the user ID, so provisioning is idempotent: it can run on
// every sign-in and only ever installs what is missing (or upgrades an untouched
// copy when a newer revision ships).

const userSpacePrefix = "ws-"

// UserKey is a short, stable, URL-safe key for a user ID.
func UserKey(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])[:12]
}

func UserFactoryProjectID(key string) string  { return userSpacePrefix + key + "-factory" }
func UserGitHubProjectID(key string) string   { return userSpacePrefix + key + "-github" }
func UserFactoryWorkflowID(key string) string { return "wf_factory-" + key }
func UserGitHubWorkflowID(key string) string  { return "wf_github-" + key }

// userKeyOf returns the user key of a per-user project ID ("ws-<key>-…"), if any.
func userKeyOf(projectID string) (string, bool) {
	if !strings.HasPrefix(projectID, userSpacePrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(projectID, userSpacePrefix)
	i := strings.IndexByte(rest, '-')
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}

// ReviewWorkflowFor returns the GitHub review workflow that should receive PRs
// opened by a run in projectID: the user's own copy for a per-user project, the
// shared built-in otherwise.
func ReviewWorkflowFor(projectID string) string {
	if key, ok := userKeyOf(projectID); ok {
		return UserGitHubWorkflowID(key)
	}
	return GitHubWorkflowID
}

// UserWorkflows returns the user's copies of the shipped factory + review
// workflows, re-homed to their projects and stamped like built-ins.
func UserWorkflows(key string) []model.Workflow {
	factory := FactoryWorkflow()
	factory.ID, factory.ProjectID = UserFactoryWorkflowID(key), UserFactoryProjectID(key)

	gh := GitHubWorkflow()
	gh.ID, gh.ProjectID = UserGitHubWorkflowID(key), UserGitHubProjectID(key)
	gh.Nodes = append([]model.Node(nil), gh.Nodes...)
	for i := range gh.Nodes {
		cfg := append([]model.Field(nil), gh.Nodes[i].Config...)
		for j := range cfg {
			// Display-only hook URL on the trigger node: point it at this copy.
			cfg[j].Value = strings.ReplaceAll(cfg[j].Value, "/api/hooks/"+GitHubWorkflowID+"?", "/api/hooks/"+gh.ID+"?")
		}
		gh.Nodes[i].Config = cfg
	}
	return []model.Workflow{factory, gh}
}

// SyncUserWorkflows installs (or cleanly upgrades) a user's workflow copies with
// the same never-overwrite-an-edit rules as SyncBuiltins. The user's projects
// must already exist.
func SyncUserWorkflows(st workflowStore, key string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	for _, wf := range UserWorkflows(key) {
		if err := syncOne(st, wf, nil, logger); err != nil {
			return err
		}
	}
	return nil
}
