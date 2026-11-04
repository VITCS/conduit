// Real Notion source for the AI Factory: query a Notion database and inject the
// first matching page (ticket) into run memory as ticket_*, so the same agent
// pipeline that works on Jira issues also works on a Notion task board. Auth is a
// project-scoped Notion integration token. Config values support ${var:…},
// ${secret:…}, ${input.…} and ${memory.…}.
package components

import (
	"encoding/json"
	"fmt"
	"strings"

	"conduit/internal/model"
)

const notionVersion = "2022-06-28"

func notionToken(n model.Node, project string) (string, bool) {
	tok := secretExpand(project, firstNonEmpty(n.Field("token"), "${secret:notion_token}"))
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.Contains(tok, "${secret:") {
		return "", false
	}
	return tok, true
}

func notionHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization":  "Bearer " + token,
		"Notion-Version": notionVersion,
		"Content-Type":   "application/json",
	}
}

// execNotion queries a database for the first page matching an optional
// property/value filter and writes its title + a chosen text property into memory.
func execNotion(n model.Node, run *model.Run) Result {
	project := runProject(run)
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }

	dbID := ex("database_id")
	if dbID == "" || strings.Contains(dbID, "${") {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "database_id is required"},
			Logs:   []string{"notion: database_id is required"}}
	}
	token, ok := notionToken(n, project)
	if !ok {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"error": "no notion token", "hint": "store a secret named notion_token (an internal integration token)"},
			Logs:   []string{"notion: no token — add a 'notion_token' secret"}}
	}

	body := map[string]any{"page_size": 1}
	if prop, val := ex("filter_property"), ex("filter_value"); prop != "" && val != "" &&
		!strings.Contains(prop, "${") && !strings.Contains(val, "${") {
		// status/select-style equals filter (the common "Ready for Dev" case).
		body["filter"] = map[string]any{
			"property": prop,
			"status":   map[string]any{"equals": val},
		}
	}

	endpoint := fmt.Sprintf("https://api.notion.com/v1/databases/%s/query", dbID)
	raw, status, err := postJSON(endpoint, notionHeaders(token), body)
	if err != nil {
		return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()},
			Logs: []string{fmt.Sprintf("notion: POST -> error: %v", err)}}
	}
	// If the status filter type is wrong for this DB, Notion 400s — retry once with a
	// select filter, which covers databases that use a Select instead of a Status.
	if status == 400 {
		if f, ok2 := body["filter"].(map[string]any); ok2 {
			f["select"] = f["status"]
			delete(f, "status")
			raw, status, err = postJSON(endpoint, notionHeaders(token), body)
			if err != nil {
				return Result{Outcome: model.StatusFailed, Output: map[string]any{"error": err.Error()}, Logs: []string{err.Error()}}
			}
		}
	}
	if status < 200 || status >= 300 {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"status_code": status, "error": apiErrSnippet(raw)},
			Logs:   []string{fmt.Sprintf("notion: query -> %d: %s", status, apiErrSnippet(raw))}}
	}

	page, found := firstNotionPage(raw, ex("description_property"))
	if !found {
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"found": false, "error": "no matching Notion page"},
			Logs:   []string{"notion: no matching page for query"}}
	}
	mem := map[string]any{
		"ticket_key":         page.id,
		"ticket_summary":     page.title,
		"ticket_description": page.description,
		"ticket_url":         page.url,
		"ticket_source":      "notion",
	}
	return Result{Outcome: model.StatusSuccess, Output: mem, MemoryWrites: mem,
		Logs: []string{fmt.Sprintf("notion: fetched %s — %s", page.id, page.title)}}
}

type notionPage struct {
	id, title, description, url string
}

// firstNotionPage extracts the first page's title (the property of type "title"),
// an optional rich_text description property, the page id and url.
func firstNotionPage(raw []byte, descProp string) (notionPage, bool) {
	var resp struct {
		Results []struct {
			ID         string                     `json:"id"`
			URL        string                     `json:"url"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Results) == 0 {
		return notionPage{}, false
	}
	r := resp.Results[0]
	p := notionPage{id: r.ID, url: r.URL}
	for name, rawProp := range r.Properties {
		var typed struct {
			Type     string       `json:"type"`
			Title    []notionRich `json:"title"`
			RichText []notionRich `json:"rich_text"`
		}
		if err := json.Unmarshal(rawProp, &typed); err != nil {
			continue
		}
		switch typed.Type {
		case "title":
			p.title = joinRich(typed.Title)
		case "rich_text":
			if descProp == "" || strings.EqualFold(descProp, name) {
				if t := joinRich(typed.RichText); t != "" {
					p.description = t
				}
			}
		}
	}
	return p, true
}

type notionRich struct {
	PlainText string `json:"plain_text"`
}

func joinRich(rs []notionRich) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(r.PlainText)
	}
	return strings.TrimSpace(b.String())
}
