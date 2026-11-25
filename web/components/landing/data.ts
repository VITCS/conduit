// Content for the marketing landing page.
//
// RULE FOR THIS FILE: every capability, integration and workflow named here must
// exist in the product (see internal/seed, internal/components, internal/api).
// The ticket, run and metric VALUES used in the animations are sample data and
// the page labels them as such.

export const REPO_URL = "https://github.com/VITCS/conduit";
export const DOCS_URL = "/docs";
export const CATALOG_URL = "/docs#catalog";

export const NAV_LINKS = [
  { href: "#demo", label: "How it runs" },
  { href: "#agents", label: "Agents" },
  { href: "#integrations", label: "Integrations" },
  { href: "#workflows", label: "Workflows" },
  { href: "#opensource", label: "Open source" },
];

/* ------------------------------------------------------------------ hero */

export type HeroNodeId = "jira" | "issues" | "model" | "approver" | "repo" | "ci" | "pr" | "notion";

// Coordinates are in the hero canvas viewBox (720 × 520); the factory hub sits in
// the middle and the systems it talks to ring it.
export const HERO_HUB = { x: 360, y: 260 };
export const HERO_NODES: { id: HeroNodeId; label: string; sub: string; x: number; y: number; tone: string }[] = [
  { id: "jira", label: "Jira", sub: "ticket source", x: 78, y: 260, tone: "blue" },
  { id: "issues", label: "GitHub Issues", sub: "ticket source", x: 158, y: 86, tone: "slate" },
  { id: "model", label: "AI model", sub: "plan · code · review", x: 360, y: 46, tone: "violet" },
  { id: "approver", label: "You", sub: "2 approval gates", x: 562, y: 86, tone: "amber" },
  { id: "repo", label: "Git branch", sub: "pushed via API", x: 642, y: 260, tone: "cyan" },
  { id: "ci", label: "CI checks", sub: "verified", x: 562, y: 434, tone: "green" },
  { id: "pr", label: "Pull request", sub: "opened", x: 360, y: 474, tone: "green" },
  { id: "notion", label: "Notion", sub: "ticket source", x: 158, y: 434, tone: "slate" },
];

// One loop of the hero run: which node is being worked with, the pulse between
// hub and node, and the log line that appears.
export const HERO_SCRIPT: {
  ms: number; node?: HeroNodeId; dir?: "in" | "out"; tag: string; text: string; tone?: string; bubble?: string;
}[] = [
  { ms: 900, node: "jira", dir: "in", tag: "intake", text: "ENG-482 received from Jira" },
  { ms: 1300, node: "model", dir: "out", tag: "triage", text: "confidence 86 · ready to automate", tone: "violet", bubble: "Triage + planning…" },
  { ms: 1500, node: "approver", dir: "out", tag: "gate 1", text: "plan approved by a human", tone: "amber", bubble: "Waiting for your approval…" },
  { ms: 1300, node: "model", dir: "out", tag: "code", text: "change plan · 4 files", tone: "violet", bubble: "Coding agent writing files…" },
  { ms: 1300, node: "approver", dir: "out", tag: "gate 2", text: "file changes approved", tone: "amber", bubble: "Waiting for your approval…" },
  { ms: 900, node: "repo", dir: "out", tag: "apply", text: "pushed branch conduit/ENG-482" },
  { ms: 1000, node: "ci", dir: "out", tag: "verify", text: "CI checks passed" },
  { ms: 900, node: "pr", dir: "out", tag: "ship", text: "pull request #129 opened", tone: "ok" },
];

/* ---------------------------------------------------------- factory demo */

export type DemoNodeId =
  | "t1" | "src" | "triage" | "plan" | "impl" | "review" | "appr"
  | "cplan" | "appr2" | "apply" | "vchk" | "pr" | "post";

export interface DemoNodeDef {
  id: DemoNodeId; label: string; sub: string; kind: "trigger" | "ai" | "action" | "gate";
  x: number; y: number; ms: number; desc: string; output: string;
}

const COL2 = 292, ROW = 88;
// The seeded "AI Factory · Ticket → PR" workflow, laid out in two columns.
export const DEMO_NODES: DemoNodeDef[] = [
  { id: "t1", label: "Ticket event", sub: "webhook · board task", kind: "trigger", x: 0, y: 0, ms: 380,
    desc: "A run starts from a board task, a Jira / Notion / GitHub ticket, or a webhook.", output: "task_id, ticket" },
  { id: "src", label: "Fetch ticket", sub: "jira", kind: "action", x: 0, y: ROW, ms: 520,
    desc: "Loads the ticket's summary and description into run memory. A board task brings its own ticket, so no fetch is needed.", output: "ticket_key, ticket_summary" },
  { id: "triage", label: "Triage · confidence", sub: "triage", kind: "ai", x: 0, y: ROW * 2, ms: 900,
    desc: "Scores how ready the ticket is to automate: scope, context, a checkable outcome. Below the threshold it goes to a person instead of an agent.", output: "confidence, confidence_score" },
  { id: "plan", label: "Refine & plan", sub: "agent", kind: "ai", x: 0, y: ROW * 3, ms: 1000,
    desc: "An agent turns the ticket into a concrete plan and stores it for the next steps.", output: "plan" },
  { id: "impl", label: "Draft implementation", sub: "agent", kind: "ai", x: 0, y: ROW * 4, ms: 900,
    desc: "An agent drafts how the plan would be implemented.", output: "implementation" },
  { id: "review", label: "Review confidence", sub: "agent", kind: "ai", x: 0, y: ROW * 5, ms: 800,
    desc: "A second agent pass reviews the draft and states how confident it is.", output: "review" },
  { id: "appr", label: "Human approval", sub: "approval · gate 1", kind: "gate", x: 0, y: ROW * 6, ms: 0,
    desc: "The run pauses. The reviewer sees the ticket, the plan and the review, then approves or rejects.", output: "approved | rejected" },
  { id: "cplan", label: "Plan changes", sub: "coding_plan", kind: "ai", x: COL2, y: 0, ms: 900,
    desc: "Produces the exact file-level change plan: which files to create or modify, and why. Nothing is written yet.", output: "change_plan" },
  { id: "appr2", label: "Approve changes", sub: "approval · gate 2", kind: "gate", x: COL2, y: ROW, ms: 0,
    desc: "The run pauses again. The reviewer approves the exact list of files before any branch exists.", output: "approved | rejected" },
  { id: "apply", label: "Apply changes", sub: "coding_apply", kind: "ai", x: COL2, y: ROW * 2, ms: 1200,
    desc: "Writes each file (one model call per file) and pushes a branch through the GitHub Git Data API. No local checkout.", output: "work_branch, commit_sha" },
  { id: "vchk", label: "Verify checks (CI)", sub: "github_checks", kind: "action", x: COL2, y: ROW * 3, ms: 1000,
    desc: "Waits for the commit's CI check-runs. On a failure it loops back to Apply changes with the failing checks as feedback, up to twice.", output: "checks_summary" },
  { id: "pr", label: "Open pull request", sub: "github_pr", kind: "action", x: COL2, y: ROW * 4, ms: 700,
    desc: "Opens the pull request from the pushed branch and hands it to the review workflow's inbox.", output: "last_pr_url" },
  { id: "post", label: "Post PR to ticket", sub: "jira", kind: "action", x: COL2, y: ROW * 5, ms: 480,
    desc: "Comments the PR link, the plan and the check results back on the ticket.", output: "comment posted" },
];
export const DEMO_ORDER: DemoNodeId[] = DEMO_NODES.map((n) => n.id);
export const DEMO_EDGES: { from: DemoNodeId; to: DemoNodeId; label?: string; side?: boolean }[] = [
  { from: "t1", to: "src" }, { from: "src", to: "triage" }, { from: "triage", to: "plan" },
  { from: "plan", to: "impl" }, { from: "impl", to: "review" }, { from: "review", to: "appr" },
  { from: "appr", to: "cplan", label: "approved", side: true },
  { from: "cplan", to: "appr2" }, { from: "appr2", to: "apply", label: "approved" },
  { from: "apply", to: "vchk" }, { from: "vchk", to: "pr", label: "success" }, { from: "pr", to: "post" },
];

export const SAMPLE_TICKETS = [
  { id: "clear" as const, key: "ENG-482", title: "Add SEO metadata, sitemap and robots.txt", score: 86,
    body: "Add title/description/Open Graph tags to every page, generate sitemap.xml and robots.txt. Done when Lighthouse SEO passes." },
  { id: "vague" as const, key: "ENG-511", title: "Make the dashboard better", score: 34,
    body: "Users say it feels off. Please improve." },
];
export const SAMPLE_PLAN = [
  "Add a shared metadata helper with title, description and Open Graph tags.",
  "Generate sitemap.xml and robots.txt from the route list.",
  "Add JSON-LD structured data to the landing page.",
];
export const SAMPLE_CHANGES = [
  { action: "modify", path: "app/layout.tsx", why: "export site metadata" },
  { action: "create", path: "app/sitemap.ts", why: "generate sitemap.xml" },
  { action: "create", path: "app/robots.ts", why: "generate robots.txt" },
  { action: "create", path: "lib/site.ts", why: "shared SEO constants" },
];

/* --------------------------------------------------------------- agents */

export const AGENTS = [
  { id: "triage", name: "Triage agent", icon: "gauge", step: "triage",
    desc: "Scores every ticket for readiness and routes ambiguous work to a person.",
    tools: ["ticket summary", "description", "threshold"], task: "Is ENG-482 scoped and checkable?",
    steps: ["Read ticket", "Score readiness", "Route: agent or human"] },
  { id: "plan", name: "Planning agent", icon: "list", step: "agent",
    desc: "Refines a ticket into a concrete, reviewable plan.",
    tools: ["ticket", "run memory"], task: "Turn the ticket into a plan",
    steps: ["Read ticket", "Refine scope", "Write plan"] },
  { id: "draft", name: "Implementation drafter", icon: "pen", step: "agent",
    desc: "Drafts how the plan should be implemented before any code is written.",
    tools: ["plan", "run memory"], task: "Draft the implementation approach",
    steps: ["Read plan", "Draft approach", "Store draft"] },
  { id: "review", name: "Review agent", icon: "check", step: "agent",
    desc: "Reviews the draft and reports its confidence, so the approver has a second opinion.",
    tools: ["plan", "draft"], task: "Review the draft and state confidence",
    steps: ["Read draft", "Check against plan", "Report confidence"] },
  { id: "coding", name: "Coding agent", icon: "code", step: "coding_plan · coding_apply",
    desc: "Plans the file changes, then writes them and pushes a real branch.",
    tools: ["GitHub Contents API", "Git Data API", "CI feedback"], task: "Write 4 files and push conduit/ENG-482",
    steps: ["Plan file changes", "Write each file", "Push branch"] },
  { id: "prreview", name: "PR review agent", icon: "git", step: "github_review",
    desc: "Reads a pull request's diff and writes the review a human then approves.",
    tools: ["PR diff", "PR comment"], task: "Review pull request #129",
    steps: ["Fetch diff", "Review changes", "Draft review"] },
];

/* --------------------------------------------------------- integrations */

// Everything here is a built-in component, trigger or backend.
export const INTEGRATIONS: { name: string; mark: string; color: string; role: string; ring: 1 | 2 }[] = [
  { name: "Jira", mark: "J", color: "#60A5FA", role: "tickets", ring: 1 },
  { name: "Notion", mark: "N", color: "#CBD5E1", role: "tickets", ring: 1 },
  { name: "GitHub", mark: "GH", color: "#9CA3AF", role: "issues · PRs · CI", ring: 1 },
  { name: "Anthropic", mark: "A", color: "#F59E6B", role: "model", ring: 1 },
  { name: "OpenAI", mark: "OA", color: "#34D399", role: "model", ring: 1 },
  { name: "Slack", mark: "S", color: "#E0A82E", role: "notify", ring: 1 },
  { name: "Webhooks", mark: "WH", color: "#22D3EE", role: "trigger", ring: 2 },
  { name: "Schedules", mark: "CR", color: "#A78BFA", role: "cron", ring: 2 },
  { name: "HTTP", mark: "{ }", color: "#8B5CF6", role: "REST", ring: 2 },
  { name: "GraphQL", mark: "GQ", color: "#F472B6", role: "API", ring: 2 },
  { name: "Model gateway", mark: "GW", color: "#FBBF24", role: "your own", ring: 2 },
  { name: "Security scan", mark: "SC", color: "#F87171", role: "PR gate", ring: 2 },
  { name: "PostgreSQL", mark: "PG", color: "#38BDF8", role: "storage", ring: 2 },
];

/* ------------------------------------------------------------ workflows */

export const WORKFLOWS = [
  { id: "factory", tab: "Ticket → PR", title: "AI Factory: a ticket becomes a pull request",
    body: "The flagship workflow. Agents triage, plan and write the change; you approve the plan and the exact files; CI must pass before the PR opens.",
    flow: ["Ticket", "Triage", "Plan", "Approve", "Plan changes", "Approve", "Apply", "Verify CI", "Open PR"],
    fact: "2 human gates", factLabel: "on every change, before any code reaches your repo" },
  { id: "review", tab: "PR review & merge", title: "GitHub: review, scan, approve, merge",
    body: "A pull request is reviewed by an agent and scanned; a person approves; Conduit posts the review and merges. PRs opened by the factory land in this workflow's inbox.",
    flow: ["PR event", "Agent review", "Security scan", "Human approval", "Post review", "Merge PR"],
    fact: "No merge", factLabel: "without a human approving the agent's review" },
  { id: "intake", tab: "Board intake", title: "Issues land on a board, scored",
    body: "A GitHub issue, Jira ticket or manual task becomes a Backlog card with a confidence score. Start it yourself, or auto-start tickets above your threshold.",
    flow: ["Issue / ticket", "Intake webhook", "Confidence score", "Backlog", "Implement", "Verify", "Done"],
    fact: "5 stages", factLabel: "Backlog · Implement · Verify · Done · Blocked" },
  { id: "custom", tab: "Build your own", title: "Any event, your steps, your gates",
    body: "Draw a workflow from the same blocks: webhook or schedule triggers, HTTP and Slack actions, conditions, waits and approval gates.",
    flow: ["Webhook / schedule", "HTTP request", "IF / filter", "Approval", "Slack"],
    fact: "30 blocks", factLabel: "component types to compose from" },
];

/* -------------------------------------------------------------- metrics */

// The fields the Metrics view reports (GET /api/metrics). Values are sample data.
export const KPIS = [
  { label: "Tasks", value: 48, suffix: "", decimals: 0, tone: "violet" },
  { label: "Done", value: 31, suffix: "", decimals: 0, tone: "ok" },
  { label: "Blocked", value: 4, suffix: "", decimals: 0, tone: "bad" },
  { label: "Avg lead time", value: 42, suffix: " min", decimals: 0, tone: "slate" },
  { label: "CI retry rate", value: 12, suffix: "%", decimals: 0, tone: "cyan" },
  { label: "Needed a human", value: 8, suffix: "%", decimals: 0, tone: "slate" },
];
export const BY_STAGE = [
  { stage: "Backlog", n: 7 }, { stage: "Implement", n: 4 }, { stage: "Verify", n: 2 },
  { stage: "Done", n: 31 }, { stage: "Blocked", n: 4 },
];
export const STAGE_DWELL = [
  { stage: "Backlog", min: 18 }, { stage: "Implement", min: 14 }, { stage: "Verify", min: 9 },
];
export const RUN_FEED = [
  { wf: "AI Factory · ENG-482", status: "ok", note: "PR #129 opened" },
  { wf: "GitHub review · #129", status: "wait", note: "awaiting approval" },
  { wf: "AI Factory · ENG-490", status: "ok", note: "CI recovered on retry" },
  { wf: "AI Factory · ENG-511", status: "fail", note: "needs clarification" },
  { wf: "AI Factory · ENG-479", status: "ok", note: "PR #127 opened" },
  { wf: "GitHub review · #127", status: "ok", note: "merged" },
] as const;

/* ------------------------------------------------------------------ faq */

export const FAQ = [
  { q: "What does Conduit actually do?",
    a: "It takes a ticket and produces a pull request. Agents triage the ticket, plan the change and write the code on a branch; you approve the plan and the exact file changes; CI has to pass; then the PR opens and is handed to a review workflow. Every step is a node on a graph you can watch live." },
  { q: "Can it push code or merge without me?",
    a: "The Ticket → PR workflow has two approval gates: one on the plan, one on the exact list of files. Nothing is written to your repository before the second approval. In the review workflow a person approves before anything is merged. At any gate you can also send the proposal back with a note on what to change; the agent revises it and you review it again. A rejection sends the run to a \"needs attention\" state." },
  { q: "Where do tickets come from?",
    a: "Jira and Notion (fetched by the workflow), GitHub issues and Jira tickets posted to the intake webhook, any webhook, or a task you add on the board by hand." },
  { q: "Which AI models does it use?",
    a: "Whichever you configure: Anthropic, OpenAI, or your own gateway. You supply the API key as an encrypted project secret and pick the model per project." },
  { q: "What happens when CI fails?",
    a: "The Verify step reads the commit's check-runs. If they fail, the run loops back to the coding agent with the failing checks as feedback, up to two times. If it still fails, the run stops and the task is marked Blocked for a person." },
  { q: "What if a step fails or I don't need one?",
    a: "Any step can be skipped with a checkbox on its node, and a failed run can be re-run from the stage that failed, keeping the work that already succeeded. You can also cancel a run at any point." },
  { q: "Is it self-hosted? Where does my code go?",
    a: "Conduit is open source and runs as a single binary on your infrastructure, with Postgres or an in-memory store. It talks to your repository through the GitHub API with a token you provide, and sends ticket and file content to the model provider you chose." },
  { q: "Do I have to use the AI Factory?",
    a: "No. The same canvas builds any event-driven workflow from the built-in blocks: webhooks, schedules, HTTP and Slack calls, conditions and approval gates." },
];
