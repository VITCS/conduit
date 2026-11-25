package components

import (
	"context"

	"conduit/internal/model"
)

// CtxExecutor is an executor that needs the run's context: long-running steps
// (Claude Code sessions, git operations) must stop when the run is canceled, and
// can report progress while they work.
type CtxExecutor func(ctx context.Context, n model.Node, run *model.Run) Result

// ctxRegistry holds component types whose executor takes a context. A type may be
// in both registries: the plain one stays for callers without a context.
var ctxRegistry = map[string]CtxExecutor{
	"coding_plan":  execCodingPlanCtx,
	"coding_apply": execCodingApplyCtx,
}

// Exec runs a node's executor, passing ctx to executors that accept one.
// It also collects the token usage of every model call the step makes.
func Exec(ctx context.Context, typ string, n model.Node, run *model.Run) Result {
	acc := beginUsage(ctx, run)
	defer endUsage(run)
	var res Result
	if e, ok := ctxRegistry[typ]; ok {
		res = e(ctx, n, run)
	} else {
		res = For(typ)(n, run)
	}
	if p := acc.chosen(); p != nil {
		if res.Output == nil {
			res.Output = map[string]any{}
		}
		if _, ok := res.Output["model_tier"]; !ok {
			p.output(res.Output)
		}
		if line := p.logLine(); line != "" && !hasLine(res.Logs, line) {
			res.Logs = append([]string{line}, res.Logs...)
		}
	}
	if u := acc.result(); u != nil {
		res.Usage = u
		if line := usageSummary(u); line != "" {
			res.Logs = append(res.Logs, line)
		}
		if res.Output == nil {
			res.Output = map[string]any{}
		}
		res.Output["tokens_in"] = u.InputTokens
		res.Output["tokens_out"] = u.OutputTokens
		if u.CostUSD > 0 {
			res.Output["cost_usd"] = u.CostUSD
		}
		if u.SubscriptionUSD > 0 {
			res.Output["cost_usd_equivalent"] = u.SubscriptionUSD
		}
	}
	return res
}

type progressKey struct{}

// WithProgress attaches a progress reporter to ctx. Executors call progress(ctx, …)
// to stream what they are doing (e.g. "Edit app/layout.tsx") while a step runs.
func WithProgress(ctx context.Context, report func(msg string)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

func progress(ctx context.Context, msg string) {
	if f, ok := ctx.Value(progressKey{}).(func(string)); ok && f != nil {
		f(msg)
	}
}

func hasLine(lines []string, l string) bool {
	for _, x := range lines {
		if x == l {
			return true
		}
	}
	return false
}
