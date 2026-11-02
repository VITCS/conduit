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
	for _, typ := range []string{"github_pr", "github_comment", "github_review", "github_merge"} {
		if !Known(typ) {
			t.Errorf("component %q not registered", typ)
		}
	}
}

func TestGitHubReviewRequiresFields(t *testing.T) {
	r := For("github_review")(node("github_review", gf("owner", "acme")), &model.Run{ProjectID: "default"})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("outcome = %s, want failed for missing fields", r.Outcome)
	}
}

func TestGitHubReviewRequiresToken(t *testing.T) {
	prev := ResolveSecret
	ResolveSecret = func(_, _ string) (string, bool) { return "", false }
	defer func() { ResolveSecret = prev }()
	n := node("github_review", gf("owner", "acme"), gf("repo", "store"), gf("number", "5"))
	r := For("github_review")(n, &model.Run{ProjectID: "default"})
	if r.Outcome != model.StatusFailed || r.Output["hint"] == nil {
		t.Fatalf("want failed with hint, got %s %v", r.Outcome, r.Output)
	}
}

func TestGitHubMergeRequiresFieldsAndToken(t *testing.T) {
	if r := For("github_merge")(node("github_merge", gf("owner", "acme")), &model.Run{ProjectID: "default"}); r.Outcome != model.StatusFailed {
		t.Fatalf("missing fields: want failed, got %s", r.Outcome)
	}
	prev := ResolveSecret
	ResolveSecret = func(_, _ string) (string, bool) { return "", false }
	defer func() { ResolveSecret = prev }()
	n := node("github_merge", gf("owner", "acme"), gf("repo", "store"), gf("number", "5"))
	if r := For("github_merge")(n, &model.Run{ProjectID: "default"}); r.Outcome != model.StatusFailed {
		t.Fatalf("no token: want failed, got %s", r.Outcome)
	}
}

func TestExpandTemplateNestedPath(t *testing.T) {
	run := &model.Run{Input: map[string]any{
		"number":     42,
		"repository": map[string]any{"name": "store", "owner": map[string]any{"login": "acme"}},
	}}
	got := expandTemplate(run, "${input.repository.owner.login}/${input.repository.name}#${input.number}")
	if got != "acme/store#42" {
		t.Fatalf("nested expand = %q, want acme/store#42", got)
	}
	// Unknown path is left verbatim.
	if v := expandTemplate(run, "${input.nope.deep}"); v != "${input.nope.deep}" {
		t.Errorf("unknown path = %q, want unchanged", v)
	}
}
