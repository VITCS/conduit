package components

// Model tiers, automatic step-up and usage/cost tracking.
//
// A project can name three models (variables model_fast, model_standard,
// model_max). Each LLM step then picks its tier: a default per component (triage
// is fast; planning, coding and review are standard), or the node's `tier` field.
// A step can step up on signals Conduit already has:
//
//   - low triage confidence, or a large change plan → the strongest model for coding steps
//   - a retry after a failed check, or a send-back from a reviewer → one tier up
//
// A node whose `model` field names a model explicitly (anything other than the
// shared ${var:llm_model} / ${var:claude_model}) always uses that model. With no
// tier variables set, every step keeps using llm_model exactly as before.
//
// Every LLM call reports token usage; the engine stores it on the step and sums it
// on the run. Cost is computed from a built-in Anthropic price table (override or
// extend with the model_prices variable).

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"conduit/internal/model"
)

const (
	tierFast     = "fast"
	tierStandard = "standard"
	tierMax      = "max"
)

var tierOrder = []string{tierFast, tierStandard, tierMax}

// defaultTier is the tier a component uses when its node sets none.
func defaultTier(typ string) string {
	switch typ {
	case "triage":
		return tierFast
	default:
		return tierStandard
	}
}

func tierIndex(t string) int {
	for i, x := range tierOrder {
		if x == t {
			return i
		}
	}
	return -1
}

func normTier(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "fast", "small", "haiku":
		return tierFast
	case "standard", "default", "medium", "sonnet":
		return tierStandard
	case "max", "best", "strong", "strongest", "opus":
		return tierMax
	}
	return ""
}

// modelPick is the model a step will use and why.
type modelPick struct {
	Model  string // "" means the provider/CLI default
	Tier   string // fast | standard | max | "" (no tiers configured / explicit model)
	Reason string // one line for the run log; "" when nothing notable
}

func (p modelPick) output(out map[string]any) {
	if p.Model != "" {
		out["model"] = p.Model
	}
	if p.Tier != "" {
		out["model_tier"] = p.Tier
	}
	if p.Reason != "" {
		out["model_reason"] = p.Reason
	}
}

// logLine is "using <model> (<tier>, <reason>)", or "" when there is nothing to say.
func (p modelPick) logLine() string {
	if p.Model == "" || (p.Tier == "" && p.Reason == "") {
		return ""
	}
	bits := []string{}
	if p.Tier != "" {
		bits = append(bits, p.Tier+" tier")
	}
	if p.Reason != "" {
		bits = append(bits, p.Reason)
	}
	return fmt.Sprintf("model: using %s (%s)", p.Model, strings.Join(bits, ", "))
}

// isSharedModelRef reports whether a node's model field just defers to the
// project's shared model variable (so tiers may apply).
func isSharedModelRef(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "" || raw == "${var:llm_model}" || raw == "${var:claude_model}"
}

// tierModels returns the project's tier → model map (only the ones that are set).
func tierModels(project string) map[string]string {
	m := map[string]string{}
	for _, t := range tierOrder {
		if v := projVar(project, "model_"+t); v != "" && !strings.Contains(v, "${") {
			m[t] = v
		}
	}
	return m
}

// pickModel resolves the model for an LLM step.
//
//	fallback  the model to use when no tiers apply (usually the expanded model field)
//	coding    whether confidence / change-plan size may step this step up
func pickModel(n model.Node, run *model.Run, fallback string, coding bool) modelPick {
	p := resolvePick(n, run, fallback, coding)
	usageFor(run).setPick(p)
	return p
}

func resolvePick(n model.Node, run *model.Run, fallback string, coding bool) modelPick {
	project := runProject(run)
	raw := n.Field("model")
	if !isSharedModelRef(raw) {
		return modelPick{Model: fallback, Reason: "set on this step"}
	}
	tiers := tierModels(project)
	if len(tiers) == 0 {
		return modelPick{Model: fallback}
	}
	tier := normTier(n.Field("tier"))
	if tier == "" {
		tier = defaultTier(n.Type)
	}
	reason := "default for this step"
	if normTier(n.Field("tier")) != "" {
		reason = "set on this step"
	}
	if up, why := stepUp(n, run, project, tier, coding); up != tier {
		tier, reason = up, why
	}
	m := tiers[tier]
	if m == "" { // tier not configured: nearest configured tier, preferring lower
		for i := tierIndex(tier); i >= 0 && m == ""; i-- {
			m = tiers[tierOrder[i]]
		}
		for i := tierIndex(tier); i < len(tierOrder) && m == ""; i++ {
			m = tiers[tierOrder[i]]
		}
	}
	if m == "" {
		return modelPick{Model: fallback}
	}
	return modelPick{Model: m, Tier: tier, Reason: reason}
}

// stepUp applies the escalation signals. It returns the (possibly higher) tier and
// the reason. Disable per project with model_escalation=off, or per node with
// escalate=false.
func stepUp(n model.Node, run *model.Run, project, tier string, coding bool) (string, string) {
	if strings.EqualFold(projVar(project, "model_escalation"), "off") || strings.EqualFold(n.Field("escalate"), "false") {
		return tier, ""
	}
	if run == nil {
		return tier, ""
	}
	up := func(t string) string {
		if i := tierIndex(t); i >= 0 && i+1 < len(tierOrder) {
			return tierOrder[i+1]
		}
		return t
	}
	// A reviewer sent this step back, or a check failed and the run looped back here.
	if retried(run, n.ID) {
		why := "retry after a failed check"
		if fb, _ := run.Memory["feedback_for_"+n.ID].(string); strings.TrimSpace(fb) != "" {
			why = "sent back by a reviewer"
		}
		if t := up(tier); t != tier {
			return t, why
		}
	}
	if !coding || tier == tierMax {
		return tier, ""
	}
	if s, ok := memNumber(run.Memory["confidence_score"]); ok {
		below := 75
		if v, err := strconv.Atoi(projVar(project, "escalate_confidence_below")); err == nil && v > 0 {
			below = v
		}
		if s < float64(below) {
			return tierMax, fmt.Sprintf("triage confidence %d is below %d", int(s), below)
		}
	}
	if files := planFileCount(run); files > 0 {
		over := 5
		if v, err := strconv.Atoi(projVar(project, "escalate_files_over")); err == nil && v > 0 {
			over = v
		}
		if files > over {
			return tierMax, fmt.Sprintf("change plan touches %d files (more than %d)", files, over)
		}
	}
	return tier, ""
}

// retried reports whether a bounded back-edge into nodeID has fired in this run.
func retried(run *model.Run, nodeID string) bool {
	for k, c := range run.Loops {
		if c > 0 && strings.HasSuffix(k, "\x00"+nodeID) {
			return true
		}
	}
	return false
}

func planFileCount(run *model.Run) int {
	raw, _ := run.Memory["change_plan_json"].(string)
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	items, perr := parseChangePlan(raw, 1000)
	if perr != "" {
		return 0
	}
	return len(items)
}

func memNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// --- prices ---------------------------------------------------------------------

// price is USD per million tokens.
type price struct {
	in, out, cacheRead float64
	// longIn/longOut apply to a call whose prompt exceeds longOver tokens (0 = never).
	longOver        int64
	longIn, longOut float64
}

// anthropicPrices are first-party Claude API list prices (USD per MTok).
// Matched by longest prefix, so dated or suffixed IDs resolve too.
var anthropicPrices = map[string]price{
	"claude-fable-5-2":  {in: 8, out: 40, cacheRead: 0.20},
	"claude-fable-5-1":  {in: 10, out: 50},
	"claude-fable-5":    {in: 10, out: 50},
	"claude-mythos-5-2": {in: 8, out: 40, cacheRead: 0.20},
	"claude-mythos-5-1": {in: 10, out: 50},
	"claude-opus-5-5":   {in: 4, out: 20, cacheRead: 0.20},
	"claude-opus-5":     {in: 5, out: 25},
	"claude-opus-4-8":   {in: 5, out: 25},
	"claude-opus-4-7":   {in: 5, out: 25},
	"claude-opus-4-6":   {in: 5, out: 25},
	"claude-sonnet-5-5": {in: 2, out: 10, cacheRead: 0.10},
	"claude-sonnet-5":   {in: 2, out: 10},
	"claude-sonnet-4-6": {in: 3, out: 15},
	"claude-haiku-5-5":  {in: 0.10, out: 0.50, longOver: 100_000, longIn: 0.50, longOut: 2.50},
	"claude-haiku-4-5":  {in: 1, out: 5},
}

// priceFor finds a model's price: the project's model_prices override first
// ("model=in/out" entries separated by commas, semicolons or newlines), then the
// built-in table.
func priceFor(project, modelName string) (price, bool) {
	m := strings.ToLower(strings.TrimSpace(modelName))
	if m == "" {
		return price{}, false
	}
	for _, entry := range strings.FieldsFunc(projVar(project, "model_prices"), func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), m) {
			continue
		}
		a, b, ok := strings.Cut(v, "/")
		in, e1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
		out, e2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
		if ok && e1 == nil && e2 == nil && in >= 0 && out >= 0 {
			return price{in: in, out: out}, true
		}
	}
	best := ""
	for k := range anthropicPrices {
		if strings.HasPrefix(m, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return price{}, false
	}
	return anthropicPrices[best], true
}

// callCost is the USD cost of one call, and whether the model had a known price.
func callCost(project, modelName string, u llmUsage) (float64, bool) {
	p, ok := priceFor(project, modelName)
	if !ok {
		return 0, false
	}
	in, out := p.in, p.out
	if p.longOver > 0 && u.input+u.cacheRead+u.cacheWrite > p.longOver {
		in, out = p.longIn, p.longOut
	}
	cr := p.cacheRead
	if cr == 0 {
		cr = in * 0.1
	}
	cost := (float64(u.input)*in + float64(u.output)*out +
		float64(u.cacheRead)*cr + float64(u.cacheWrite)*in*1.25) / 1e6
	return cost, true
}

// --- usage collection -------------------------------------------------------------

// llmUsage is the token usage one provider call reported.
type llmUsage struct {
	input, output, cacheRead, cacheWrite int64
}

// usageAcc collects the usage of every LLM call one step makes.
type usageAcc struct {
	mu      sync.Mutex
	project string
	u       model.Usage
	pick    *modelPick
}

// setPick remembers the step's model choice, so Exec can report it.
func (a *usageAcc) setPick(p modelPick) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.pick = &p
	a.mu.Unlock()
}

func (a *usageAcc) chosen() *modelPick {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pick
}

func (a *usageAcc) add(modelName string, u llmUsage) {
	if a == nil {
		return
	}
	cost, priced := callCost(a.project, modelName, u)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.u.Calls++
	a.u.InputTokens += u.input
	a.u.OutputTokens += u.output
	a.u.CacheReadTokens += u.cacheRead
	a.u.CacheWriteTokens += u.cacheWrite
	if priced {
		a.u.CostUSD += cost
	} else {
		a.u.Unpriced++
	}
	if modelName != "" {
		a.u.Model = modelName
	}
}

// addReported records a session that reported its own cost (Claude Code). With a
// subscription the work is not billed per token, so the cost is kept apart as the
// equivalent API cost.
func (a *usageAcc) addReported(modelName string, u llmUsage, costUSD float64, subscription bool) {
	if a == nil {
		return
	}
	if costUSD <= 0 {
		if c, ok := callCost(a.project, modelName, u); ok {
			costUSD = c
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.u.Calls++
	a.u.InputTokens += u.input
	a.u.OutputTokens += u.output
	a.u.CacheReadTokens += u.cacheRead
	a.u.CacheWriteTokens += u.cacheWrite
	if subscription {
		a.u.SubscriptionUSD += costUSD
	} else {
		a.u.CostUSD += costUSD
	}
	if modelName != "" {
		a.u.Model = modelName
	}
}

func (a *usageAcc) result() *model.Usage {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.u.Calls == 0 {
		return nil
	}
	u := a.u
	u.CostUSD = roundUSD(u.CostUSD)
	u.SubscriptionUSD = roundUSD(u.SubscriptionUSD)
	return &u
}

func roundUSD(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// Executors get a fresh run snapshot per call, so the snapshot pointer identifies
// one step execution. Exec registers a collector for it; LLM helpers find it with
// usageFor(run).
var usageByRun sync.Map // *model.Run → *usageAcc

func beginUsage(run *model.Run) *usageAcc {
	if run == nil {
		return nil
	}
	a := &usageAcc{project: runProject(run)}
	usageByRun.Store(run, a)
	return a
}

func endUsage(run *model.Run) {
	if run != nil {
		usageByRun.Delete(run)
	}
}

// usageFor returns the step's collector, or nil (recording is then a no-op).
func usageFor(run *model.Run) *usageAcc {
	if run == nil {
		return nil
	}
	if a, ok := usageByRun.Load(run); ok {
		return a.(*usageAcc)
	}
	return nil
}

// usageSummary is a one-line log entry, e.g. "usage: 1 call · 1,204 in / 380 out · $0.0062".
func usageSummary(u *model.Usage) string {
	if u == nil {
		return ""
	}
	calls := "calls"
	if u.Calls == 1 {
		calls = "call"
	}
	s := fmt.Sprintf("usage: %d %s · %s in / %s out", u.Calls, calls, commas(u.InputTokens), commas(u.OutputTokens))
	switch {
	case u.SubscriptionUSD > 0:
		s += fmt.Sprintf(" · ~$%.4f at API prices (subscription, not billed)", u.SubscriptionUSD)
	case u.CostUSD > 0:
		s += fmt.Sprintf(" · $%.4f", u.CostUSD)
	}
	if u.Unpriced > 0 {
		s += " · no price for " + firstNonEmpty(u.Model, "this model") + " (set model_prices)"
	}
	return s
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// AddUsage adds b into a run total, keeping a per-model breakdown.
func AddUsage(total *model.Usage, b *model.Usage) *model.Usage {
	if b == nil {
		return total
	}
	if total == nil {
		total = &model.Usage{}
	}
	merge := func(dst *model.Usage, src *model.Usage) {
		dst.Calls += src.Calls
		dst.InputTokens += src.InputTokens
		dst.OutputTokens += src.OutputTokens
		dst.CacheReadTokens += src.CacheReadTokens
		dst.CacheWriteTokens += src.CacheWriteTokens
		dst.CostUSD = roundUSD(dst.CostUSD + src.CostUSD)
		dst.SubscriptionUSD = roundUSD(dst.SubscriptionUSD + src.SubscriptionUSD)
		dst.Unpriced += src.Unpriced
	}
	merge(total, b)
	if total.ByModel == nil {
		total.ByModel = map[string]*model.Usage{}
	}
	key := firstNonEmpty(b.Model, "(default)")
	m := total.ByModel[key]
	if m == nil {
		m = &model.Usage{Model: key}
		total.ByModel[key] = m
	}
	merge(m, b)
	return total
}

// SortedModels lists a usage breakdown's models, most expensive first.
func SortedModels(by map[string]*model.Usage) []*model.Usage {
	out := make([]*model.Usage, 0, len(by))
	for _, u := range by {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := out[i].CostUSD+out[i].SubscriptionUSD, out[j].CostUSD+out[j].SubscriptionUSD
		if ci != cj {
			return ci > cj
		}
		return out[i].Model < out[j].Model
	})
	return out
}
