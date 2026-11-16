// Content + demo data for the marketing landing page.
//
// Everything in this file is presentation data. Numbers, leads, run history and
// outcomes are ILLUSTRATIVE demo data (the page labels them as such) — nothing
// here is read from a live deployment.

export const REPO_URL = "https://github.com/VITCS/conduit";
// "Talk to our team" target. Point this at a real contact channel (a mailto:,
// a scheduling link, …) for your deployment.
export const CONTACT_URL = `${REPO_URL}/issues/new`;
export const DOCS_URL = "/docs";
export const CATALOG_URL = "/docs#catalog";

export const NAV_LINKS = [
  { href: "#demo", label: "Product" },
  { href: "#agents", label: "Agents" },
  { href: "#integrations", label: "Integrations" },
  { href: "#usecases", label: "Use cases" },
  { href: "#pricing", label: "Pricing" },
];

/* ------------------------------------------------------------------ hero */

export type HeroNodeId =
  | "webhooks" | "database" | "agent" | "gmail" | "slack" | "sheets" | "crm" | "notion";

// Coordinates are in the hero canvas viewBox (720 × 520). The orchestrator hub
// sits in the middle; application nodes ring it.
export const HERO_HUB = { x: 360, y: 260 };
export const HERO_NODES: { id: HeroNodeId; label: string; sub: string; x: number; y: number; tone: string }[] = [
  { id: "webhooks", label: "Webhooks", sub: "trigger", x: 78, y: 260, tone: "cyan" },
  { id: "database", label: "Database", sub: "store", x: 158, y: 86, tone: "slate" },
  { id: "agent", label: "AI Agent", sub: "reason", x: 360, y: 46, tone: "violet" },
  { id: "gmail", label: "Gmail", sub: "send", x: 562, y: 86, tone: "rose" },
  { id: "slack", label: "Slack", sub: "notify", x: 642, y: 260, tone: "amber" },
  { id: "sheets", label: "Google Sheets", sub: "append", x: 562, y: 434, tone: "green" },
  { id: "crm", label: "CRM", sub: "upsert", x: 360, y: 474, tone: "blue" },
  { id: "notion", label: "Notion", sub: "log", x: 158, y: 434, tone: "slate" },
];
export const HERO_OUTPUTS: HeroNodeId[] = ["gmail", "slack", "sheets", "crm", "notion", "database"];

/* ----------------------------------------------------------- lead demo */

export type LeadNodeId =
  | "form" | "analyze" | "enrich" | "dedupe" | "score" | "decide"
  | "crm" | "slack" | "email" | "nurture" | "followup";

export interface LeadNodeDef {
  id: LeadNodeId; label: string; sub: string; kind: "trigger" | "ai" | "action" | "logic";
  x: number; y: number; ms: number; desc: string; input: string; output: string;
}

export const LEAD_NODES: LeadNodeDef[] = [
  { id: "form", label: "Form submitted", sub: "Webhook trigger", kind: "trigger", x: 0, y: 0, ms: 420,
    desc: "A website form posts to a workflow webhook. The JSON body becomes the run's input.",
    input: "POST /api/hooks/wf_leads", output: "{ name, email, company, message }" },
  { id: "analyze", label: "AI analyzes lead", sub: "AI Agent", kind: "ai", x: 0, y: 88, ms: 1100,
    desc: "An agent reads the message, extracts intent and urgency, and writes a short summary to run memory.",
    input: "${input.message}", output: "{ intent, urgency, summary }" },
  { id: "enrich", label: "Enrich lead", sub: "HTTP request", kind: "action", x: 0, y: 176, ms: 760,
    desc: "Calls an enrichment API for company size, industry and role. Retries automatically on a failed attempt.",
    input: "${input.email}", output: "{ company_size, industry, role }" },
  { id: "dedupe", label: "Check duplicates", sub: "CRM lookup", kind: "action", x: 0, y: 264, ms: 540,
    desc: "Looks the contact up in the CRM so an existing record is updated instead of duplicated.",
    input: "email", output: "{ exists: false }" },
  { id: "score", label: "Assign lead score", sub: "AI Agent", kind: "ai", x: 0, y: 352, ms: 900,
    desc: "Combines intent, enrichment and history into a 0–100 score that routes the lead.",
    input: "${memory.summary} + enrichment", output: "{ score }" },
  { id: "decide", label: "Score ≥ 70 ?", sub: "IF / ELSE", kind: "logic", x: 0, y: 440, ms: 320,
    desc: "A conditional node. Its outcome decides which labelled edge the run follows.",
    input: "${memory.score}", output: "high | low" },
  { id: "crm", label: "Add to CRM", sub: "High priority", kind: "action", x: -128, y: 560, ms: 520,
    desc: "Creates the deal and assigns an owner.", input: "lead + score", output: "{ deal_id }" },
  { id: "slack", label: "Notify sales", sub: "Slack", kind: "action", x: -128, y: 648, ms: 420,
    desc: "Posts the lead summary to the sales channel.", input: "${memory.summary}", output: "message sent" },
  { id: "email", label: "Personalized email", sub: "AI Agent + email", kind: "ai", x: -128, y: 736, ms: 820,
    desc: "An agent drafts a reply from the lead's own words; the email step sends it.", input: "summary + role", output: "email sent" },
  { id: "nurture", label: "Add to nurture", sub: "Low priority", kind: "action", x: 128, y: 560, ms: 480,
    desc: "Adds the contact to a nurture campaign.", input: "email", output: "campaign joined" },
  { id: "followup", label: "Schedule follow-up", sub: "Wait + schedule", kind: "logic", x: 128, y: 648, ms: 420,
    desc: "Schedules a check-in so the lead isn't forgotten.", input: "+14 days", output: "scheduled" },
];

export const LEAD_CHAIN: LeadNodeId[] = ["form", "analyze", "enrich", "dedupe", "score", "decide"];
export const LEAD_BRANCH: Record<"high" | "low", LeadNodeId[]> = {
  high: ["crm", "slack", "email"],
  low: ["nurture", "followup"],
};
export const LEAD_EDGES: { from: LeadNodeId; to: LeadNodeId; label?: string }[] = [
  { from: "form", to: "analyze" }, { from: "analyze", to: "enrich" }, { from: "enrich", to: "dedupe" },
  { from: "dedupe", to: "score" }, { from: "score", to: "decide" },
  { from: "decide", to: "crm", label: "score ≥ 70" }, { from: "crm", to: "slack" }, { from: "slack", to: "email" },
  { from: "decide", to: "nurture", label: "else" }, { from: "nurture", to: "followup" },
];

export const SAMPLE_LEADS = [
  { id: "high" as const, name: "Dana Whitfield", company: "Northwind Logistics", role: "VP Engineering", score: 87,
    message: "We need to automate onboarding for 40 customers a month — can we talk this week?" },
  { id: "low" as const, name: "Sam Ortiz", company: "Personal project", role: "Student", score: 31,
    message: "Just curious how this works. Is there a free tier?" },
];

/* --------------------------------------------------------------- agents */

export const AGENTS = [
  { id: "research", name: "Research Agent", icon: "search", desc: "Gathers sources, reads them and returns a cited brief.",
    tools: ["HTTP", "GraphQL", "Notion"], task: "Summarize this week's competitor launches",
    steps: ["Search sources", "Read & extract", "Write brief"] },
  { id: "sales", name: "Sales Agent", icon: "handshake", desc: "Qualifies inbound leads and drafts the first reply.",
    tools: ["CRM", "Slack", "Email"], task: "Score the lead and draft a personal reply",
    steps: ["Read lead", "Score fit", "Draft reply"] },
  { id: "support", name: "Customer Support Agent", icon: "headphones", desc: "Triages tickets and answers from your own docs.",
    tools: ["Webhooks", "Notion", "Slack"], task: "Classify the ticket and suggest a fix",
    steps: ["Classify", "Find answer", "Reply / escalate"] },
  { id: "analyst", name: "Data Analyst Agent", icon: "chart", desc: "Pulls numbers, explains the change, flags anomalies.",
    tools: ["GraphQL", "HTTP", "Sheets"], task: "Explain yesterday's drop in sign-ups",
    steps: ["Query data", "Compare periods", "Explain"] },
  { id: "marketing", name: "Marketing Agent", icon: "megaphone", desc: "Turns one brief into channel-ready drafts for review.",
    tools: ["Notion", "Slack", "HTTP"], task: "Draft launch posts from the release notes",
    steps: ["Read brief", "Draft variants", "Send for approval"] },
  { id: "ops", name: "Operations Agent", icon: "settings", desc: "Watches systems and runs the routine response.",
    tools: ["Webhooks", "SSH", "GitHub"], task: "Health check failed — open an incident",
    steps: ["Detect", "Diagnose", "Open incident"] },
];

/* --------------------------------------------------------- integrations */

// support: how Conduit reaches the tool today.
//   native — a built-in component; api — through the HTTP / GraphQL / webhook
//   components with your own credentials; planned — not available yet.
export type Support = "native" | "api" | "planned";
export const INTEGRATIONS: { name: string; mark: string; color: string; support: Support; ring: 1 | 2 }[] = [
  { name: "Slack", mark: "S", color: "#E0A82E", support: "native", ring: 1 },
  { name: "GitHub", mark: "GH", color: "#9CA3AF", support: "native", ring: 1 },
  { name: "Notion", mark: "N", color: "#CBD5E1", support: "native", ring: 1 },
  { name: "OpenAI", mark: "OA", color: "#34D399", support: "native", ring: 1 },
  { name: "Anthropic", mark: "A", color: "#F59E6B", support: "native", ring: 1 },
  { name: "Webhooks", mark: "WH", color: "#22D3EE", support: "native", ring: 1 },
  { name: "REST APIs", mark: "{ }", color: "#8B5CF6", support: "native", ring: 2 },
  { name: "PostgreSQL", mark: "PG", color: "#60A5FA", support: "native", ring: 2 },
  { name: "Gmail", mark: "M", color: "#F87171", support: "api", ring: 2 },
  { name: "Google Sheets", mark: "GS", color: "#34D399", support: "api", ring: 2 },
  { name: "Airtable", mark: "AT", color: "#FBBF24", support: "api", ring: 2 },
  { name: "HubSpot", mark: "HS", color: "#FB923C", support: "api", ring: 2 },
  { name: "Salesforce", mark: "SF", color: "#38BDF8", support: "api", ring: 2 },
  { name: "Discord", mark: "D", color: "#818CF8", support: "api", ring: 2 },
  { name: "MySQL", mark: "MY", color: "#94A3B8", support: "planned", ring: 2 },
];
export const SUPPORT_LABEL: Record<Support, string> = {
  native: "Built in",
  api: "Via HTTP / webhook blocks",
  planned: "Planned",
};
export const SUPPORT_SHORT: Record<Support, string> = { native: "Built in", api: "Via API", planned: "Planned" };

/* ------------------------------------------------------------ use cases */

export const USE_CASES = [
  { id: "sales", tab: "Sales & Marketing", title: "Route every lead in seconds",
    body: "Score inbound leads with an agent, enrich them, and hand hot ones to sales with a drafted reply.",
    flow: ["Form submitted", "AI scores lead", "Enrich", "IF score ≥ 70", "CRM + Slack", "Draft email"],
    metric: "4 min → 20 sec", metricLabel: "lead response time" },
  { id: "support", tab: "Customer Support", title: "Triage before a human opens the ticket",
    body: "Classify, tag and answer routine tickets from your docs; escalate the rest with context attached.",
    flow: ["Ticket webhook", "AI classifies", "Search docs", "IF confident", "Reply", "Else escalate"],
    metric: "62%", metricLabel: "tickets resolved without a hand-off" },
  { id: "eng", tab: "Software Engineering", title: "Tickets in, reviewed pull requests out",
    body: "Conduit's AI Factory triages a ticket, plans the change, writes the branch, verifies CI and opens a PR — with two human approval gates.",
    flow: ["Ticket", "Triage", "Plan", "Approve", "Coding agent", "Verify CI", "Open PR"],
    metric: "2 gates", metricLabel: "human approvals on every change" },
  { id: "finance", tab: "Finance & Operations", title: "Close the loop on invoices",
    body: "Extract invoice fields, match them to purchase orders, and send only the exceptions for approval.",
    flow: ["Invoice received", "AI extracts fields", "Match PO", "IF mismatch", "Approval gate", "Post to ledger"],
    metric: "3 days → 4 hrs", metricLabel: "invoice cycle time" },
  { id: "hr", tab: "Human Resources", title: "Onboarding that runs itself",
    body: "One signed offer triggers accounts, equipment requests, a welcome plan and day-one reminders.",
    flow: ["Offer signed", "Create accounts", "Request equipment", "Welcome plan", "Schedule check-ins"],
    metric: "11 steps", metricLabel: "automated per new hire" },
  { id: "bi", tab: "Business Intelligence", title: "A daily brief nobody has to build",
    body: "Pull metrics on a schedule, let an agent explain what moved, and post the brief where the team reads it.",
    flow: ["Schedule 9:00", "Query metrics", "AI explains change", "IF anomaly", "Alert", "Post brief"],
    metric: "0 dashboards", metricLabel: "to check before stand-up" },
];

/* ------------------------------------------------------------ analytics */

export const KPIS = [
  { label: "Workflows executed", value: 18432, suffix: "", decimals: 0, tone: "violet" },
  { label: "Tasks automated", value: 126870, suffix: "", decimals: 0, tone: "cyan" },
  { label: "Successful", value: 98.7, suffix: "%", decimals: 1, tone: "ok" },
  { label: "Failed", value: 239, suffix: "", decimals: 0, tone: "bad" },
  { label: "Avg execution", value: 3.4, suffix: "s", decimals: 1, tone: "slate" },
  { label: "Time saved", value: 1284, suffix: " h", decimals: 0, tone: "ok" },
];

export const EXEC_SERIES = [
  { d: "Mon", ok: 1120, fail: 14 }, { d: "Tue", ok: 1280, fail: 19 }, { d: "Wed", ok: 1210, fail: 11 },
  { d: "Thu", ok: 1460, fail: 22 }, { d: "Fri", ok: 1390, fail: 16 }, { d: "Sat", ok: 870, fail: 7 },
  { d: "Sun", ok: 790, fail: 6 }, { d: "Mon", ok: 1340, fail: 15 }, { d: "Tue", ok: 1510, fail: 18 },
  { d: "Wed", ok: 1580, fail: 13 }, { d: "Thu", ok: 1720, fail: 21 }, { d: "Fri", ok: 1650, fail: 12 },
  { d: "Sat", ok: 980, fail: 8 }, { d: "Sun", ok: 1030, fail: 9 },
];
export const TIME_SAVED = [
  { team: "Sales", h: 312 }, { team: "Support", h: 286 }, { team: "Eng", h: 244 },
  { team: "Finance", h: 198 }, { team: "HR", h: 121 }, { team: "BI", h: 123 },
];
export const LIVE_FEED = [
  { wf: "Lead routing", status: "ok", ms: 2140 }, { wf: "Ticket triage", status: "ok", ms: 1380 },
  { wf: "Invoice match", status: "wait", ms: 0 }, { wf: "Daily brief", status: "ok", ms: 4820 },
  { wf: "AI Factory · PR", status: "ok", ms: 61200 }, { wf: "Onboarding", status: "ok", ms: 3310 },
  { wf: "Lead routing", status: "fail", ms: 910 }, { wf: "Health check", status: "ok", ms: 640 },
] as const;

/* -------------------------------------------------------------- pricing */

// Prices are PLACEHOLDERS — set real values (or remove the section) before
// publishing. `price: null` renders as "Custom".
export const PRICING: {
  id: string; name: string; blurb: string; price: string | null; period: string;
  cta: "start" | "contact"; featured?: boolean; features: string[];
}[] = [
  { id: "starter", name: "Starter", blurb: "For individuals exploring automation.", price: "$—", period: "/ month",
    cta: "start",
    features: ["Visual workflow builder", "Core components & webhooks", "Human approval gates", "Community support"] },
  { id: "pro", name: "Professional", blurb: "For growing teams and businesses.", price: "$—", period: "/ user / month",
    cta: "start", featured: true,
    features: ["Everything in Starter", "AI agents on your own model keys", "Projects, roles & members",
      "Schedules, retries & run history", "Priority support"] },
  { id: "ent", name: "Enterprise", blurb: "Advanced orchestration, security and governance.", price: null, period: "",
    cta: "contact",
    features: ["Everything in Professional", "Self-hosted, single binary", "SSO sign-in & audit trail",
      "Encrypted secrets & egress controls", "Deployment guidance"] },
];
export const PRICING_COMPARE: { feature: string; values: [string, string, string] }[] = [
  { feature: "Workflows", values: ["Placeholder", "Placeholder", "Unlimited"] },
  { feature: "AI agent steps", values: ["—", "Included", "Included"] },
  { feature: "Human approval gates", values: ["Included", "Included", "Included"] },
  { feature: "Projects & roles", values: ["1 project", "Multiple", "Multiple + SSO"] },
  { feature: "Audit trail", values: ["—", "—", "Included"] },
];

/* ------------------------------------------------------------------ faq */

export const FAQ = [
  { q: "What is AI workflow automation?",
    a: "It's a workflow — triggers, steps and conditions — where some steps are performed by an AI agent. In Conduit you draw the workflow as a graph; agent steps read context, decide and write results that later steps use." },
  { q: "Do I need coding experience?",
    a: "No to build and run workflows: you connect blocks on a canvas and fill in fields. Connecting a tool that isn't built in uses the HTTP or webhook block, which means knowing that tool's API URL and key." },
  { q: "Can I connect my existing applications?",
    a: "Yes. Slack, GitHub, Jira, Notion and the major model providers are built in. Anything with an HTTP API or webhooks connects through the HTTP, GraphQL and webhook blocks using credentials you store as encrypted secrets." },
  { q: "How do AI agents work?",
    a: "An agent step sends a templated prompt — filled with the trigger payload and results from earlier steps — to the model you choose (Anthropic, OpenAI or your own gateway) and stores the answer for the next step. The workflow graph, not the model, decides what happens next." },
  { q: "Can workflows run automatically?",
    a: "Yes. A workflow starts from a webhook, a schedule, an event from a connected tool, or a manual run. Queued runs are picked up by workers and executed without anyone watching." },
  { q: "Can I include human approval steps?",
    a: "Yes. An approval gate pauses the run, shows the reviewer what the agent proposed, and continues only after Approve — or follows the rejected path." },
  { q: "How do I monitor workflow executions?",
    a: "Every run streams live onto the canvas: each step shows its state, output and logs. Run history, a task board and metrics are built in, and a failed step can be re-run from where it stopped." },
  { q: "Is my data secure?",
    a: "Conduit is self-hosted, so workflows and data stay on your infrastructure. Secrets are encrypted at rest, access is role-based per project, and outbound requests to private networks are blocked unless you allow them." },
];
