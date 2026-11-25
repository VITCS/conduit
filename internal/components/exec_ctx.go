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
func Exec(ctx context.Context, typ string, n model.Node, run *model.Run) Result {
	if e, ok := ctxRegistry[typ]; ok {
		return e(ctx, n, run)
	}
	return For(typ)(n, run)
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
