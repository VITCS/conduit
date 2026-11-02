package components

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"conduit/internal/model"
)

// execAgent runs an LLM "agent" step. By default it is simulated (deterministic,
// offline) so the seeded factory workflow and tests run without a provider. With
// live:true it calls a real provider (anthropic/openai/custom) using a
// project-scoped secret for the API key.
//
// The prompt may template run state: ${input.x} and ${memory.key}.
func execAgent(n model.Node, run *model.Run) Result {
	project := runProject(run)
	provider := strings.ToLower(firstNonEmpty(n.Field("provider"), "anthropic"))
	modelName := n.Field("model")
	prompt := expandTemplate(run, secretExpand(project, firstNonEmpty(n.Field("prompt"), n.Field("instruction"))))

	if !strings.EqualFold(n.Field("live"), "true") {
		// Simulated agent — deterministic structured response.
		conf := firstNonEmpty(n.Field("confidence"), "high")
		text := fmt.Sprintf("[simulated %s agent] %s", provider, summarize(prompt))
		return Result{
			Outcome: model.StatusSuccess,
			Output: map[string]any{
				"provider": provider, "model": firstNonEmpty(modelName, "(default)"),
				"text": text, "confidence": conf, "simulated": true,
			},
			Logs: []string{fmt.Sprintf("agent(%s) simulated response", provider)},
		}
	}

	apiKey := secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("key")))
	baseURL := n.Field("base_url")
	maxTokens := 1024
	if v, err := strconv.Atoi(n.Field("max_tokens")); err == nil && v > 0 {
		maxTokens = v
	}
	text, status, err := callLLM(provider, baseURL, apiKey, modelName, prompt, maxTokens)
	if err != nil || status < 200 || status >= 300 {
		msg := "request failed"
		if err != nil {
			msg = err.Error()
		}
		return Result{
			Outcome: model.StatusFailed,
			Output:  map[string]any{"provider": provider, "status_code": status, "error": msg},
			Logs:    []string{fmt.Sprintf("agent(%s) failed: %s (status %d)", provider, msg, status)},
		}
	}
	return Result{
		Outcome: model.StatusSuccess,
		Output:  map[string]any{"provider": provider, "model": modelName, "status_code": status, "text": text},
		Logs:    []string{fmt.Sprintf("agent(%s) ok (%d chars)", provider, len(text))},
	}
}

// callLLM dispatches to a provider adapter and returns (text, statusCode, err).
func callLLM(provider, baseURL, apiKey, modelName, prompt string, maxTokens int) (string, int, error) {
	switch provider {
	case "openai":
		return callOpenAI(firstNonEmpty(baseURL, "https://api.openai.com"), apiKey, firstNonEmpty(modelName, "gpt-4o-mini"), prompt)
	case "custom":
		return callCustom(baseURL, apiKey, modelName, prompt)
	default: // anthropic
		return callAnthropic(firstNonEmpty(baseURL, "https://api.anthropic.com"), apiKey, firstNonEmpty(modelName, "claude-3-5-sonnet-latest"), prompt, maxTokens)
	}
}

func postJSON(url string, headers map[string]string, body any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
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

func callAnthropic(base, apiKey, modelName, prompt string, maxTokens int) (string, int, error) {
	body := map[string]any{
		"model": modelName, "max_tokens": maxTokens,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	}
	raw, status, err := postJSON(strings.TrimRight(base, "/")+"/v1/messages", map[string]string{
		"x-api-key": apiKey, "anthropic-version": "2023-06-01",
	}, body)
	if err != nil {
		return "", status, err
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(raw, &r)
	var sb strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), status, nil
}

func callOpenAI(base, apiKey, modelName, prompt string) (string, int, error) {
	body := map[string]any{
		"model":    modelName,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	}
	raw, status, err := postJSON(strings.TrimRight(base, "/")+"/v1/chat/completions", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, body)
	if err != nil {
		return "", status, err
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(raw, &r)
	if len(r.Choices) > 0 {
		return r.Choices[0].Message.Content, status, nil
	}
	return "", status, nil
}

// callCustom posts {prompt} and expects {text} — for self-hosted/gateway models.
func callCustom(base, apiKey, modelName, prompt string) (string, int, error) {
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	raw, status, err := postJSON(base, headers, map[string]any{"model": modelName, "prompt": prompt})
	if err != nil {
		return "", status, err
	}
	var r struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Text, status, nil
}

// --- prompt templating ---

var tmplRe = regexp.MustCompile(`\$\{(input|memory)\.([A-Za-z0-9_.\-]+)\}`)

// expandTemplate substitutes ${input.KEY} and ${memory.KEY} from the run. KEY may
// be a dotted path into nested objects, so a webhook payload resolves naturally —
// e.g. ${input.repository.owner.login} or ${input.pull_request.number}.
func expandTemplate(run *model.Run, s string) string {
	if run == nil || !strings.Contains(s, "${") {
		return s
	}
	return tmplRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := tmplRe.FindStringSubmatch(tok)
		src, key := m[1], m[2]
		var bag map[string]any
		if src == "input" {
			bag = run.Input
		} else {
			bag = run.Memory
		}
		if v, ok := lookupPath(bag, key); ok {
			return fmt.Sprintf("%v", v)
		}
		return tok
	})
}

// lookupPath resolves a dotted key against nested map[string]any objects (the
// shape JSON unmarshals to). A plain key still works as a single lookup.
func lookupPath(bag map[string]any, key string) (any, bool) {
	if bag == nil {
		return nil, false
	}
	if v, ok := bag[key]; ok { // exact (also covers keys that contain dots)
		return v, true
	}
	parts := strings.Split(key, ".")
	var cur any = bag
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func summarize(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if len(s) > 140 {
		return s[:140] + "…"
	}
	if s == "" {
		return "(no prompt)"
	}
	return s
}
