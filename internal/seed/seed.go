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

// GitHubProjectID holds the single, unified GitHub PR workflow.
const GitHubProjectID = "github"

// GitHubWorkflow is one workflow covering the whole PR lifecycle, so you never
// switch projects to open vs. review a PR. A single webhook trigger fans out to two
// branches, selected by the payload:
//   - a manual "open" request ({"branch":"..."}) opens a real pull request;
//   - a GitHub pull_request event (opened/reopened/synchronize) runs an agent
//     review -> human approval -> posts the review -> merges.
//
// Each branch's filter skips the irrelevant payload, so only one path runs.
// Configure once: project vars `owner`/`repo` (Config panel) and secrets
// `github_token` (PR + Contents: write) and `llm_key` (for a real LLM review).
func GitHubWorkflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_github",
		ProjectID: GitHubProjectID,
		Name:      "GitHub · Open + Review PRs",
		Nodes: []model.Node{
			{ID: "t1", Type: "webhook", Icon: "github", Kind: model.KindTrigger, Title: "GitHub Event", X: 40, Y: 340,
				Config: []model.Field{f("on", "github", "pull_request / manual open"), f("hook", "webhook", "POST /api/hooks/wf_github?token=…")}},

			// ── Branch A: open a PR on demand (payload carries a branch) ──
			{ID: "fo", Type: "filter", Icon: "branch", Kind: model.KindStep, Title: "Open request?", X: 300, Y: 150,
				Config: []model.Field{f("value", "key", "${input.branch}"), f("not_empty", "branch", "true")}},
			{ID: "pr", Type: "github_pr", Icon: "github", Kind: model.KindStep, Title: "Open Pull Request", X: 560, Y: 150,
				Config: []model.Field{
					f("owner", "github", "${var:owner}"),
					f("repo", "github", "${var:repo}"),
					f("head", "branch", "${input.branch}"),
					f("base", "branch", "main"),
					f("title", "file", "Automated PR: ${input.branch}"),
					f("body", "file", "Opened automatically by Conduit."),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "rec", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record PR", X: 820, Y: 150,
				Config: []model.Field{f("store", "db", "prs"), f("key", "key", "branch"), f("set", "file", "last_pr_url")}},

			// ── Branch B: review + approve + merge on a pull_request event ──
			{ID: "fr", Type: "filter", Icon: "branch", Kind: model.KindStep, Title: "On opened / updated", X: 300, Y: 470,
				Config: []model.Field{f("value", "key", "${input.action}"), f("in", "branch", "opened, reopened, synchronize")}},
			{ID: "rev", Type: "github_review", Icon: "agent", Kind: model.KindStep, Title: "Agent Review", X: 560, Y: 470,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("provider", "agent", "anthropic"),
					f("model", "agent", "claude-3-5-sonnet-latest"),
					f("api_key", "key", "${secret:llm_key}"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "scan", Type: "security_scan", Icon: "approve", Kind: model.KindStep, Title: "Security Scan", X: 840, Y: 470,
				Config: []model.Field{
					f("base_url", "globe", "${var:audit_url}"),
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("ref", "branch", "${input.pull_request.head.ref}"),
					f("timeout_s", "clock", "600"),
					f("token", "key", "${secret:scan_token}"),
				}},
			{ID: "appr", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Human Approval", X: 1120, Y: 470,
				Config: []model.Field{f("approvers", "user", "maintainers"), f("timeout", "clock", "1d")}},
			{ID: "cmt", Type: "github_comment", Icon: "github", Kind: model.KindStep, Title: "Post Review", X: 1400, Y: 360,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("body", "file", "🤖 Agent review:\n\n${memory.review}"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "merge", Type: "github_merge", Icon: "github", Kind: model.KindStep, Title: "Merge PR", X: 1680, Y: 360,
				Config: []model.Field{
					f("owner", "github", "${input.repository.owner.login}"),
					f("repo", "github", "${input.repository.name}"),
					f("number", "key", "${input.number}"),
					f("method", "key", "squash"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "sf", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Changes Requested", X: 1120, Y: 660,
				Config: []model.Field{f("set", "github", "changes_requested"), f("notify", "user", "author")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "fo"},
			{From: "t1", To: "fr"},
			{From: "fo", To: "pr", Label: "success"},
			{From: "pr", To: "rec", Label: "success"},
			{From: "fr", To: "rev", Label: "success"},
			{From: "rev", To: "scan", Label: "success"},
			{From: "rev", To: "sf", Label: "failed"},
			{From: "scan", To: "appr", Label: "success"},
			{From: "scan", To: "sf", Label: "failed"},
			{From: "appr", To: "cmt", Label: "approved"},
			{From: "appr", To: "sf", Label: "failed"},
			{From: "cmt", To: "merge", Label: "success"},
		},
	}
}

// FactoryProjectID is the project that holds the AI Factory sample workflow.
const FactoryProjectID = "ai-factory"

// FactoryWorkflow returns the AI Factory pipeline: a real ticket is pulled from
// Jira (or Notion), a model refines it into a plan, drafts an implementation, and
// reviews it; a human approves; then the plan is posted back onto the ticket.
//
// Like the GitHub workflow, every connection is configured once as project config
// — vars (${var:jira_base}, ${var:jira_jql}, …) and secrets (${secret:jira_token},
// ${secret:llm_key}) — so each node just displays what it reads. Swap the Jira
// source node for the Notion node from the palette to pull from a Notion database
// instead; both write the same ticket_* memory keys the rest of the pipeline uses.
func FactoryWorkflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_factory",
		ProjectID: FactoryProjectID,
		Name:      "AI Factory · Ticket → PR",
		Nodes: []model.Node{
			{ID: "t1", Type: "webhook", Icon: "webhook", Kind: model.KindTrigger, Title: "Ticket Event", X: 40, Y: 300,
				Config: []model.Field{f("on", "webhook", "jira / notion / manual run"), f("hook", "webhook", "POST /api/hooks/wf_factory?token=…")}},
			{ID: "src", Type: "jira", Icon: "ticket", Kind: model.KindStep, Title: "Fetch Jira Ticket", X: 320, Y: 300,
				Config: []model.Field{
					f("base_url", "globe", "${var:jira_base}"),
					f("email", "user", "${var:jira_email}"),
					f("jql", "file", "${var:jira_jql}"),
					f("api_token", "key", "${secret:jira_token}"),
				}},
			{ID: "plan", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Refine & Plan", X: 600, Y: 300,
				Config: []model.Field{
					f("live", "agent", "true"),
					f("provider", "agent", "${var:llm_provider}"),
					f("model", "agent", "${var:llm_model}"),
					f("api_key", "key", "${secret:llm_key}"),
					f("output", "db", "plan"),
					f("prompt", "file", "You are a senior engineer. Turn this ticket into a concrete, step-by-step implementation plan.\n\nTicket ${memory.ticket_key}: ${memory.ticket_summary}\n\n${memory.ticket_description}"),
				}},
			{ID: "impl", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Draft Implementation", X: 880, Y: 300,
				Config: []model.Field{
					f("live", "agent", "true"),
					f("provider", "agent", "${var:llm_provider}"),
					f("model", "agent", "${var:llm_model}"),
					f("api_key", "key", "${secret:llm_key}"),
					f("output", "db", "implementation"),
					f("prompt", "file", "Given this plan, outline the concrete code changes (files, functions, tests) needed.\n\n${memory.plan}"),
				}},
			{ID: "review", Type: "agent", Icon: "agent", Kind: model.KindStep, Title: "Review Confidence", X: 1160, Y: 300,
				Config: []model.Field{
					f("live", "agent", "true"),
					f("provider", "agent", "${var:llm_provider}"),
					f("model", "agent", "${var:llm_model}"),
					f("api_key", "key", "${secret:llm_key}"),
					f("output", "db", "review"),
					f("prompt", "file", "Review this plan and implementation for correctness and risk. State a merge-confidence (high/medium/low) and any blockers.\n\nPLAN:\n${memory.plan}\n\nIMPLEMENTATION:\n${memory.implementation}"),
				}},
			{ID: "appr", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Human Approval", X: 1440, Y: 300,
				Config: []model.Field{
					f("approvers", "user", "maintainers"),
					f("timeout", "clock", "1d"),
					// Shown in the approval notification + banner so the reviewer sees
					// exactly what the agent proposed before deciding.
					f("summary", "file", "Ticket ${memory.ticket_key} — ${memory.ticket_summary}\n\n— Plan —\n${memory.plan}\n\n— Review —\n${memory.review}"),
				}},
			{ID: "cplan", Type: "coding_plan", Icon: "code", Kind: model.KindStep, Title: "Plan Changes", X: 1680, Y: 300,
				Config: []model.Field{
					f("owner", "github", "${var:owner}"),
					f("repo", "github", "${var:repo}"),
					f("provider", "agent", "${var:llm_provider}"),
					f("model", "agent", "${var:llm_model}"),
					f("api_key", "key", "${secret:llm_key}"),
					f("plan", "file", "${memory.plan}"),
					f("implementation", "file", "${memory.implementation}"),
				}},
			{ID: "appr2", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Approve Changes", X: 1920, Y: 300,
				Config: []model.Field{
					f("approvers", "user", "maintainers"),
					f("timeout", "clock", "1d"),
					// The exact file-level change plan, approved before any branch exists.
					f("summary", "file", "Proposed file changes for ${memory.ticket_key}:\n\n${memory.change_plan}"),
				}},
			{ID: "apply", Type: "coding_apply", Icon: "code", Kind: model.KindStep, Title: "Apply Changes", X: 2160, Y: 300,
				Config: []model.Field{
					f("owner", "github", "${var:owner}"),
					f("repo", "github", "${var:repo}"),
					f("base", "branch", "main"),
					f("provider", "agent", "${var:llm_provider}"),
					f("model", "agent", "${var:llm_model}"),
					f("api_key", "key", "${secret:llm_key}"),
					f("token", "key", "${secret:github_token}"),
					f("changes", "file", "${memory.change_plan_json}"),
					f("plan", "file", "${memory.plan}"),
					f("ticket", "key", "${memory.ticket_key}"),
					f("ticket_summary", "file", "${memory.ticket_summary}"),
					f("branch_prefix", "branch", "conduit/"),
				}},
			{ID: "pr", Type: "github_pr", Icon: "github", Kind: model.KindStep, Title: "Open Pull Request", X: 2400, Y: 300,
				Config: []model.Field{
					f("owner", "github", "${var:owner}"),
					f("repo", "github", "${var:repo}"),
					f("head", "branch", "${memory.work_branch}"),
					f("base", "branch", "main"),
					f("title", "file", "feat: ${memory.ticket_key} ${memory.ticket_summary}"),
					f("body", "file", "Automated by the AI Factory for ${memory.ticket_key}.\n\n— Change plan —\n${memory.change_plan}\n\n— Plan —\n${memory.plan}\n\n— Review —\n${memory.review}"),
					f("token", "key", "${secret:github_token}"),
				}},
			{ID: "post", Type: "jira", Icon: "ticket", Kind: model.KindStep, Title: "Post PR to Ticket", X: 2680, Y: 200,
				Config: []model.Field{
					f("action", "agent", "comment"),
					f("base_url", "globe", "${var:jira_base}"),
					f("email", "user", "${var:jira_email}"),
					f("issue_key", "key", "${memory.ticket_key}"),
					f("api_token", "key", "${secret:jira_token}"),
					f("body", "file", "🤖 AI Factory opened a PR: ${memory.last_pr_url}\n\n— Plan —\n${memory.plan}"),
				}},
			{ID: "mem", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record Delivery", X: 2680, Y: 420,
				Config: []model.Field{f("store", "db", "deliveries"), f("key", "key", "ticket_key"), f("set", "file", "work_branch, last_pr_url")}},
			{ID: "fail", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "No ticket / Not approved", X: 600, Y: 540,
				Config: []model.Field{f("set", "github", "idle"), f("notify", "user", "author")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "src"},
			{From: "src", To: "plan", Label: "success"},
			{From: "src", To: "fail", Label: "failed"},
			{From: "plan", To: "impl", Label: "success"},
			{From: "impl", To: "review", Label: "success"},
			{From: "review", To: "appr", Label: "success"},
			{From: "appr", To: "cplan", Label: "approved"},
			{From: "appr", To: "fail", Label: "failed"},
			{From: "cplan", To: "appr2", Label: "success"},
			{From: "cplan", To: "fail", Label: "failed"},
			{From: "appr2", To: "apply", Label: "approved"},
			{From: "appr2", To: "fail", Label: "failed"},
			{From: "apply", To: "pr", Label: "success"},
			{From: "apply", To: "fail", Label: "failed"},
			{From: "pr", To: "post", Label: "success"},
			{From: "post", To: "mem", Label: "success"},
		},
	}
}
