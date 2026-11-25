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
	// ex resolves a field through ${var:…}/${secret:…} (project config) and
	// ${input.…}/${memory.…} (run state) — so provider and model can be set once as
	// project variables (e.g. provider ${var:llm_provider}, model ${var:llm_model}),
	// exactly like the coding/triage components.
	ex := func(field string) string { return expandTemplate(run, secretExpand(project, n.Field(field))) }
	provider := strings.ToLower(firstNonEmpty(ex("provider"), "anthropic"))
	pick := pickModel(n, run, ex("model"), false)
	modelName := pick.Model
	prompt := expandTemplate(run, secretExpand(project, firstNonEmpty(n.Field("prompt"), n.Field("instruction"))))
	// Every agent gets the project context, and a step sent back by a reviewer
	// gets the feedback plus its previous answer.
	prompt = contextBlock(project) + prompt + reviewerFeedback(run, n.ID, "text")

	// An optional `output` field names a memory key to store the agent's text in,
	// so steps chain: Refine -> ${memory.plan} -> Implement, etc.
	outKey := strings.TrimSpace(n.Field("output"))
	mem := func(text string) map[string]any {
		if outKey == "" {
			return nil
		}
		return map[string]any{outKey: text}
	}

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
			MemoryWrites: mem(text),
			Logs:         []string{fmt.Sprintf("agent(%s) simulated response", provider)},
		}
	}

	apiKey := secretExpand(project, firstNonEmpty(n.Field("api_key"), n.Field("key")))
	baseURL := ex("base_url")
	// Fail fast with guidance if a ${var:…}/${secret:…} reference didn't resolve,
	// rather than sending a literal "${var:llm_model}" to the provider (a confusing
	// 404). This usually means the project variable isn't set for THIS project.
	if unresolved := firstUnresolved(provider, modelName, baseURL, apiKey); unresolved != "" {
		msg := fmt.Sprintf("unresolved config %q — set the project's variables/secrets (e.g. llm_provider, llm_model, llm_key) for this project, with those exact names", unresolved)
		return Result{Outcome: model.StatusFailed,
			Output: map[string]any{"provider": provider, "error": msg},
			Logs:   []string{"agent: " + msg}}
	}
	// A planning/review agent routinely needs more than a sentence; the old 1024
	// default truncated "Refine & Plan". Default generously; a node can raise it
	// further via max_tokens (or lower it to cap cost).
	maxTokens := 4096
	if v, err := strconv.Atoi(ex("max_tokens")); err == nil && v > 0 {
		maxTokens = v
	}
	text, status, err := callLLMRec(usageFor(run), provider, baseURL, apiKey, modelName, prompt, maxTokens)
	logs := []string{}
	if l := pick.logLine(); l != "" {
		logs = append(logs, l)
	}
	if err != nil || status < 200 || status >= 300 {
		msg := "request failed"
		if err != nil {
			msg = err.Error()
		}
		out := map[string]any{"provider": provider, "status_code": status, "error": msg}
		pick.output(out)
		return Result{
			Outcome: model.StatusFailed,
			Output:  out,
			Logs:    append(logs, fmt.Sprintf("agent(%s) failed: %s (status %d)", provider, msg, status)),
		}
	}
	out := map[string]any{"provider": provider, "model": modelName, "status_code": status, "text": text}
	pick.output(out)
	return Result{
		Outcome:      model.StatusSuccess,
		Output:       out,
		MemoryWrites: mem(text),
		Logs:         append(logs, fmt.Sprintf("agent(%s) ok (%d chars)", provider, len(text))),
	}
}

// callLLM dispatches to a provider adapter and returns (text, statusCode, err).
func callLLM(provider, baseURL, apiKey, modelName, prompt string, maxTokens int) (string, int, error) {
	text, status, _, _, err := callLLMUsage(provider, baseURL, apiKey, modelName, prompt, maxTokens)
	return text, status, err
}

// callLLMRec is callLLM that also records the call's token usage on acc (the
// step's collector; nil records nothing).
func callLLMRec(acc *usageAcc, provider, baseURL, apiKey, modelName, prompt string, maxTokens int) (string, int, error) {
	text, status, u, used, err := callLLMUsage(provider, baseURL, apiKey, modelName, prompt, maxTokens)
	if u != (llmUsage{}) {
		acc.add(used, u)
	}
	return text, status, err
}

// callLLMUsage is callLLM plus the reported token usage and the model that ran.
func callLLMUsage(provider, baseURL, apiKey, modelName, prompt string, maxTokens int) (string, int, llmUsage, string, error) {
	switch provider {
	case "openai":
		m := firstNonEmpty(modelName, "gpt-4o-mini")
		text, status, u, err := callOpenAI(firstNonEmpty(baseURL, "https://api.openai.com"), apiKey, m, prompt)
		return text, status, u, m, err
	case "custom":
		text, status, u, err := callCustom(baseURL, apiKey, modelName, prompt)
		return text, status, u, modelName, err
	default: // anthropic
		m := firstNonEmpty(modelName, defaultAnthropicModel)
		text, status, u, err := callAnthropic(firstNonEmpty(baseURL, "https://api.anthropic.com"), apiKey, m, prompt, maxTokens)
		return text, status, u, m, err
	}
}

// defaultAnthropicModel is used when no model is configured anywhere.
const defaultAnthropicModel = "claude-sonnet-5-5"

// postJSON POSTs JSON using the standard (fast) client — for quick API calls.
func postJSON(url string, headers map[string]string, body any) ([]byte, int, error) {
	return postJSONWith(liveHTTPClient, url, headers, body)
}

// postJSONWith POSTs JSON using a caller-chosen client (LLM calls use the
// long-timeout llmHTTPClient; completions can take tens of seconds).
func postJSONWith(client *http.Client, url string, headers map[string]string, body any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return out, resp.StatusCode, nil
}

func callAnthropic(base, apiKey, modelName, prompt string, maxTokens int) (string, int, llmUsage, error) {
	body := map[string]any{
		"model": modelName, "max_tokens": maxTokens,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	}
	raw, status, err := postJSONWith(llmHTTPClient, strings.TrimRight(base, "/")+"/v1/messages", map[string]string{
		"x-api-key": apiKey, "anthropic-version": "2023-06-01",
	}, body)
	if err != nil {
		return "", status, llmUsage{}, err
	}
	if status < 200 || status >= 300 {
		return "", status, llmUsage{}, fmt.Errorf("anthropic %d: %s", status, apiErrSnippet(raw))
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(raw, &r)
	u := llmUsage{input: r.Usage.InputTokens, output: r.Usage.OutputTokens,
		cacheRead: r.Usage.CacheReadInputTokens, cacheWrite: r.Usage.CacheCreationInputTokens}
	var sb strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if sb.Len() == 0 {
		return "", status, u, fmt.Errorf("anthropic returned no text: %s", apiErrSnippet(raw))
	}
	if r.StopReason == "max_tokens" {
		return sb.String(), status, u, fmt.Errorf("response truncated at max_tokens=%d — raise max_tokens on this node (or lower max_files)", maxTokens)
	}
	return sb.String(), status, u, nil
}

func callOpenAI(base, apiKey, modelName, prompt string) (string, int, llmUsage, error) {
	body := map[string]any{
		"model":    modelName,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	}
	raw, status, err := postJSONWith(llmHTTPClient, strings.TrimRight(base, "/")+"/v1/chat/completions", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, body)
	if err != nil {
		return "", status, llmUsage{}, err
	}
	if status < 200 || status >= 300 {
		return "", status, llmUsage{}, fmt.Errorf("openai %d: %s", status, apiErrSnippet(raw))
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(raw, &r)
	u := llmUsage{input: r.Usage.PromptTokens, output: r.Usage.CompletionTokens}
	if len(r.Choices) > 0 && r.Choices[0].Message.Content != "" {
		if r.Choices[0].FinishReason == "length" {
			return r.Choices[0].Message.Content, status, u, fmt.Errorf("response truncated (finish_reason=length) — raise max_tokens on this node (or lower max_files)")
		}
		return r.Choices[0].Message.Content, status, u, nil
	}
	return "", status, u, fmt.Errorf("openai returned no text: %s", apiErrSnippet(raw))
}

// callCustom posts {prompt} and expects {text} — for self-hosted/gateway models.
// An optional usage object ({input_tokens, output_tokens}) is recorded.
func callCustom(base, apiKey, modelName, prompt string) (string, int, llmUsage, error) {
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	raw, status, err := postJSONWith(llmHTTPClient, base, headers, map[string]any{"model": modelName, "prompt": prompt})
	if err != nil {
		return "", status, llmUsage{}, err
	}
	if status < 200 || status >= 300 {
		return "", status, llmUsage{}, fmt.Errorf("custom llm %d: %s", status, apiErrSnippet(raw))
	}
	var r struct {
		Text  string `json:"text"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(raw, &r)
	u := llmUsage{input: r.Usage.InputTokens, output: r.Usage.OutputTokens}
	if r.Text == "" {
		return "", status, u, fmt.Errorf("custom llm returned no text: %s", apiErrSnippet(raw))
	}
	return r.Text, status, u, nil
}

// apiErrSnippet returns a short, single-line excerpt of an API error body for logs.
func apiErrSnippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

// --- prompt templating ---

var tmplRe = regexp.MustCompile(`\$\{(input|memory)\.([A-Za-z0-9_.\-]+)\}`)

// Render substitutes ${input.KEY} / ${memory.KEY} against a run (no secrets). It
// is the exported form used by the engine to render approval summaries.
func Render(run *model.Run, s string) string { return expandTemplate(run, s) }

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

// firstUnresolved returns the first value that still contains a ${var:…} or
// ${secret:…} token (i.e. a reference that didn't resolve), or "" if all are clean.
func firstUnresolved(vals ...string) string {
	for _, v := range vals {
		if strings.Contains(v, "${var:") || strings.Contains(v, "${secret:") {
			return v
		}
	}
	return ""
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
