package api

import "testing"

func TestNormalizeIntakeGitHub(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{
			"number": float64(482), "title": "Add rate limiting", "body": "Throttle the API.",
			"html_url": "https://github.com/o/r/issues/482",
		},
		"repository": map[string]any{"full_name": "o/r"},
	}
	n := normalizeIntake("github", raw)
	if n.Source != "github" || n.Title != "Add rate limiting" || n.SourceRef != "o/r#482" {
		t.Fatalf("github normalize: %+v", n)
	}
	if n.SourceURL != "https://github.com/o/r/issues/482" || n.Description != "Throttle the API." {
		t.Errorf("github fields: %+v", n)
	}
}

func TestNormalizeIntakeJira(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{
			"key":    "ENG-42",
			"fields": map[string]any{"summary": "Fix login", "description": "SSO broken"},
		},
	}
	n := normalizeIntake("jira", raw)
	if n.Source != "jira" || n.SourceRef != "ENG-42" || n.Title != "Fix login" || n.Description != "SSO broken" {
		t.Fatalf("jira normalize: %+v", n)
	}
}

func TestNormalizeIntakeGeneric(t *testing.T) {
	raw := map[string]any{"title": "Do thing", "description": "details", "source": "notion", "source_ref": "NT-1"}
	n := normalizeIntake("", raw)
	if n.Title != "Do thing" || n.Source != "notion" || n.SourceRef != "NT-1" {
		t.Fatalf("generic normalize: %+v", n)
	}
}
