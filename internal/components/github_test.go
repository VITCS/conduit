package components

import (
	"testing"

	"conduit/internal/model"
)

func gf(k, v string) model.Field { return model.Field{Key: k, Value: v} }

func TestGitHubPRRequiresFields(t *testing.T) {
	// Missing owner/repo/head/title -> failed, no network call.
	r := For("github_pr")(node("github_pr", gf("owner", "acme")), &model.Run{ProjectID: "default"})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("outcome = %s, want failed for missing fields", r.Outcome)
	}
}

func TestGitHubPRRequiresToken(t *testing.T) {
	// All fields present but no resolvable github_token -> failed with a hint.
	prev := ResolveSecret
	ResolveSecret = func(_, _ string) (string, bool) { return "", false }
	defer func() { ResolveSecret = prev }()

	n := node("github_pr", gf("owner", "acme"), gf("repo", "store"), gf("head", "feature"), gf("title", "Add X"))
	r := For("github_pr")(n, &model.Run{ProjectID: "default"})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("outcome = %s, want failed without a token", r.Outcome)
	}
	if r.Output["hint"] == nil {
		t.Error("expected a hint about the github_token secret")
	}
}

func TestGitHubCommentRequiresFields(t *testing.T) {
	r := For("github_comment")(node("github_comment", gf("owner", "acme"), gf("repo", "store")), &model.Run{ProjectID: "default"})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("outcome = %s, want failed for missing number/body", r.Outcome)
	}
}

func TestGitHubComponentsRegistered(t *testing.T) {
	for _, typ := range []string{"github_pr", "github_comment"} {
		if !Known(typ) {
			t.Errorf("component %q not registered", typ)
		}
	}
}
