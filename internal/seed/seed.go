// Package seed provides the default workflow so a fresh server opens on a
// realistic canvas rather than an empty one. It mirrors the "Preview
// Environments" pipeline the Conduit UI ships with.
package seed

import "conduit/internal/model"

func f(key, icon, value string) model.Field { return model.Field{Key: key, Icon: icon, Value: value} }

// Workflow returns the default "Preview Environments" DAG.
func Workflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_preview_envs",
		ProjectID: model.DefaultProjectID,
		Name:      "Preview Environments · DigitalOcean",
		Nodes: []model.Node{
			{ID: "t1", Type: "github", Icon: "github", Kind: model.KindTrigger, Title: "On Pull Request", X: 40, Y: 300,
				Config: []model.Field{f("repo", "github", "storejs / storefront"), f("on", "webhook", "pull_request.opened")}},
			{ID: "g1", Type: "timegate", Icon: "clock", Kind: model.KindStep, Title: "Business Hours Gate", X: 330, Y: 300,
				Config: []model.Field{f("window", "clock", "Mon–Fri 09–18"), f("tz", "globe", "Europe/Lisbon")}},
			{ID: "p1", Type: "http", Icon: "server", Kind: model.KindStep, Title: "Provision Server", X: 620, Y: 300,
				Config: []model.Field{f("region", "pin", "nyc1"), f("size", "size", "s-1vcpu-1gb"), f("image", "img", "ubuntu-24-04-x64")}},
			{ID: "c1", Type: "ssh", Icon: "terminal", Kind: model.KindStep, Title: "Configure App", X: 910, Y: 300,
				Config: []model.Field{f("host", "server", "159.89.230.13"), f("run", "file", "scripts/preview-setup.sh")}},
			{ID: "h1", Type: "http", Icon: "heart", Kind: model.KindStep, Title: "Health Check", X: 1200, Y: 300,
				Config: []model.Field{f("url", "globe", "http://159.89.230.13/healthz"), f("expect", "approve", "200 · 3 retries")}},
			{ID: "a1", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Require Approval", X: 1200, Y: 560,
				Config: []model.Field{f("approvers", "user", "on-call · product"), f("timeout", "clock", "4h")}},
			{ID: "u1", Type: "http", Icon: "globe", Kind: model.KindStep, Title: "Publish Preview URL", X: 1490, Y: 560,
				Config: []model.Field{f("target", "github", "PR #154 comment"), f("url", "globe", "pr-154.preview.storejs.dev")}},
			{ID: "m1", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record Environment", X: 1490, Y: 300,
				Config: []model.Field{f("store", "db", "preview-envs"), f("key", "key", "pr_number"), f("set", "file", "droplet_id, url, sha")}},
			{ID: "sf", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Mark Failed", X: 910, Y: 560,
				Config: []model.Field{f("set", "github", "failure"), f("notify", "user", "author + on-call")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "g1"},
			{From: "g1", To: "p1"},
			{From: "p1", To: "c1", Label: "success"},
			{From: "c1", To: "h1", Label: "success"},
			{From: "c1", To: "sf", Label: "failed"},
			{From: "h1", To: "m1", Label: "success"},
			{From: "h1", To: "a1", Label: "success"},
			{From: "h1", To: "sf", Label: "failed"},
			{From: "a1", To: "u1", Label: "approved"},
		},
	}
}

// PRProjectID is the project that holds the "Open a real PR" starter workflow.
const PRProjectID = "github-pr"

// OpenPRWorkflow is a ready-to-use starter that opens a REAL GitHub pull request.
// It is inert until configured: set a project secret named `github_token` and edit
// the "Open Pull Request" node's `owner`/`repo` fields. Trigger it with a webhook
// whose body carries the branch, e.g. {"branch":"feature/login"}; the title/head
// pull it in via ${input.branch}. The approval gate demonstrates the human-in-the-
// loop before a real side effect occurs.
func OpenPRWorkflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_open_pr",
		ProjectID: PRProjectID,
		Name:      "Open a real PR · GitHub",
		Nodes: []model.Node{
			{ID: "t1", Type: "webhook", Icon: "webhook", Kind: model.KindTrigger, Title: "On Webhook", X: 40, Y: 320,
				Config: []model.Field{f("on", "webhook", "POST /api/hooks/wf_open_pr"), f("body", "file", `{"branch":"feature/login"}`)}},
			{ID: "a1", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Review & Approve", X: 340, Y: 320,
				Config: []model.Field{f("approvers", "user", "maintainers"), f("timeout", "clock", "1d")}},
			{ID: "pr", Type: "github_pr", Icon: "github", Kind: model.KindStep, Title: "Open Pull Request", X: 650, Y: 320,
				Config: []model.Field{
					f("owner", "github", "${var:owner}"),
					f("repo", "github", "${var:repo}"),
					f("head", "branch", "${input.branch}"),
					f("base", "branch", "main"),
					f("title", "file", "Automated PR: ${input.branch}"),
					f("body", "file", "Opened automatically by Conduit."),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "rec", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record PR", X: 960, Y: 200,
				Config: []model.Field{f("store", "db", "prs"), f("key", "key", "branch"), f("set", "file", "last_pr_url")}},
			{ID: "sf", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Mark Failed", X: 960, Y: 460,
				Config: []model.Field{f("set", "github", "failure"), f("notify", "user", "maintainers")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "a1"},
			{From: "a1", To: "pr", Label: "approved"},
			{From: "pr", To: "rec", Label: "success"},
			{From: "pr", To: "sf", Label: "failed"},
		},
	}
}

// PRReviewProjectID holds the agent PR-review + human-approval workflow.
const PRReviewProjectID = "pr-review"

// PRReviewWorkflow is the "agent reviews a real PR, a human approves, then it
// merges" flow. It is driven by a GitHub pull_request webhook: the payload's repo
// and PR number flow into every node via ${input.*}. Inert until configured — set
// a `github_token` secret (PR read/write + Contents: write to merge) and, for a
// real LLM review, an `llm_key` secret (otherwise the review step is simulated).
func PRReviewWorkflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_pr_review",
		ProjectID: PRReviewProjectID,
		Name:      "PR Review · Agent + Human",
		Nodes: []model.Node{
			{ID: "t1", Type: "webhook", Icon: "github", Kind: model.KindTrigger, Title: "On Pull Request", X: 40, Y: 320,
				Config: []model.Field{f("on", "github", "pull_request"), f("hook", "webhook", "POST /api/hooks/wf_pr_review?token=…")}},
			// Only review on opened/reopened/synchronize — so a merge (closed) event
			// doesn't start yet another run. Other actions skip and the run ends.
			{ID: "flt", Type: "filter", Icon: "branch", Kind: model.KindStep, Title: "On opened / updated", X: 300, Y: 320,
				Config: []model.Field{f("value", "key", "${input.action}"), f("in", "branch", "opened, reopened, synchronize")}},
			{ID: "rev", Type: "github_review", Icon: "agent", Kind: model.KindStep, Title: "Agent Review", X: 560, Y: 320,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("provider", "agent", "anthropic"),
					f("model", "agent", "claude-3-5-sonnet-latest"),
					f("api_key", "key", "${secret:llm_key}"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "appr", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Human Approval", X: 840, Y: 320,
				Config: []model.Field{f("approvers", "user", "maintainers"), f("timeout", "clock", "1d")}},
			{ID: "cmt", Type: "github_comment", Icon: "github", Kind: model.KindStep, Title: "Post Review", X: 1120, Y: 200,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("body", "file", "🤖 Agent review:\n\n${memory.review}"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "merge", Type: "github_merge", Icon: "github", Kind: model.KindStep, Title: "Merge PR", X: 1400, Y: 200,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("method", "key", "squash"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "sf", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Changes Requested", X: 1120, Y: 460,
				Config: []model.Field{f("set", "github", "changes_requested"), f("notify", "user", "author")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "flt"},
			{From: "flt", To: "rev", Label: "success"},
			{From: "rev", To: "appr", Label: "success"},
			{From: "rev", To: "sf", Label: "failed"},
			{From: "appr", To: "cmt", Label: "approved"},
			{From: "appr", To: "sf", Label: "failed"},
			{From: "cmt", To: "merge", Label: "success"},
		},
	}
}

// FactoryProjectID is the project that holds the AI Factory sample workflow.
const FactoryProjectID = "ai-factory"

// FactoryWorkflow returns an AI "factory" pipeline: an issue is refined, a coding
// agent implements it, CI verifies, a review agent assesses confidence, a human
// approves, and a PR is opened. Agent steps are simulated by default (offline);
// set live:true + an api_key secret on them to call a real model.
func FactoryWorkflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_factory",
		ProjectID: FactoryProjectID,
		Name:      "AI Factory · Issue → Reviewed PR",
		Nodes: []model.Node{
			{ID: "t1", Type: "github", Icon: "github", Kind: model.KindTrigger, Title: "On Issue Labeled", X: 40, Y: 300,
				Config: []model.Field{f("repo", "github", "storejs / storefront"), f("on", "webhook", "issues.labeled: agent")}},
			{ID: "plan", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Refine & Plan", X: 330, Y: 300,
				Config: []model.Field{f("provider", "agent", "anthropic"), f("prompt", "file", "Refine issue ${input.issue} into a concrete plan")}},
			{ID: "impl", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Implement (coding agent)", X: 620, Y: 300,
				Config: []model.Field{f("provider", "agent", "anthropic"), f("prompt", "file", "Implement the plan and open a draft change")}},
			{ID: "ci", Type: "http", Icon: "heart", Kind: model.KindStep, Title: "Verify (CI)", X: 910, Y: 300,
				Config: []model.Field{f("url", "globe", "https://ci.example/run"), f("expect", "approve", "200 · tests green")}},
			{ID: "review", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Review Confidence", X: 1200, Y: 300,
				Config: []model.Field{f("provider", "agent", "anthropic"), f("prompt", "file", "Assess merge confidence for the change"), f("confidence", "approve", "high")}},
			{ID: "appr", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Human Approval", X: 1490, Y: 300,
				Config: []model.Field{f("approvers", "user", "maintainers"), f("timeout", "clock", "1d")}},
			{ID: "pr", Type: "http", Icon: "github", Kind: model.KindStep, Title: "Open Pull Request", X: 1780, Y: 300,
				Config: []model.Field{f("target", "github", "create PR"), f("title", "file", "fix: ${input.issue}")}},
			{ID: "mem", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record Delivery", X: 1780, Y: 540,
				Config: []model.Field{f("store", "db", "deliveries"), f("key", "key", "issue"), f("set", "file", "pr_url, confidence")}},
			{ID: "fail", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Send Back to Agent", X: 910, Y: 540,
				Config: []model.Field{f("set", "github", "needs-work"), f("notify", "user", "author")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "plan"},
			{From: "plan", To: "impl", Label: "success"},
			{From: "impl", To: "ci", Label: "success"},
			{From: "ci", To: "review", Label: "success"},
			{From: "ci", To: "fail", Label: "failed"},
			{From: "review", To: "appr", Label: "success"},
			{From: "appr", To: "pr", Label: "approved"},
			{From: "pr", To: "mem", Label: "success"},
		},
	}
}
