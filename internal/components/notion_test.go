package components

import (
	"testing"

	"conduit/internal/model"
)

func notionNode(fields map[string]string) model.Node {
	var cfg []model.Field
	for k, v := range fields {
		cfg = append(cfg, model.Field{Key: k, Value: v})
	}
	return model.Node{ID: "src", Type: "notion", Config: cfg}
}

func TestNotionMissingToken(t *testing.T) {
	r := execNotion(notionNode(map[string]string{"database_id": "db123"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without token, got %s", r.Outcome)
	}
}

func TestNotionQuery(t *testing.T) {
	// Point the Notion base at a test server via the public hostname override is
	// not needed: the component builds api.notion.com directly, so we instead test
	// through a transport swap by using the same code path with a stubbed URL is
	// not possible — assert the parser on a representative response body instead.
	body := []byte(`{"results":[{"id":"page-1","url":"https://notion.so/page-1","properties":{
		"Name":{"type":"title","title":[{"plain_text":"Fix login bug"}]},
		"Description":{"type":"rich_text","rich_text":[{"plain_text":"Users cannot log in via SSO."}]}
	}}]}`)
	p, ok := firstNotionPage(body, "Description")
	if !ok {
		t.Fatal("expected a page")
	}
	if p.title != "Fix login bug" {
		t.Errorf("title = %q", p.title)
	}
	if p.description != "Users cannot log in via SSO." {
		t.Errorf("description = %q", p.description)
	}
	if p.id != "page-1" {
		t.Errorf("id = %q", p.id)
	}
}

func TestNotionMissingDatabase(t *testing.T) {
	r := execNotion(notionNode(map[string]string{"token": "ntn"}), &model.Run{})
	if r.Outcome != model.StatusFailed {
		t.Fatalf("want failed without database_id, got %s", r.Outcome)
	}
}

func TestNotionParsesTitleWithoutDescriptionFilter(t *testing.T) {
	body := []byte(`{"results":[{"id":"p2","url":"u","properties":{"Task":{"type":"title","title":[{"plain_text":"Ship it"}]}}}]}`)
	p, ok := firstNotionPage(body, "")
	if !ok || p.title != "Ship it" {
		t.Errorf("got %+v ok=%v", p, ok)
	}
}
