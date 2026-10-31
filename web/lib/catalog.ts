import type { Kind, Status } from "./types";

// The component palette: what you can drop onto the canvas.
export interface CatalogItem {
  type: string;
  name: string;
  icon: string;
  kind: Kind;
  desc: string;
}

export const CATALOG: CatalogItem[] = [
  { type: "github", name: "Git event", icon: "github", kind: "trigger", desc: "Pull request / push" },
  { type: "webhook", name: "Webhook", icon: "webhook", kind: "trigger", desc: "Inbound HTTP trigger" },
  { type: "schedule", name: "Schedule", icon: "clock", kind: "trigger", desc: "Cron schedule" },
  { type: "http", name: "HTTP", icon: "globe", kind: "step", desc: "Call an API" },
  { type: "graphql", name: "GraphQL", icon: "globe", kind: "step", desc: "Query a GraphQL API" },
  { type: "slack", name: "Slack", icon: "webhook", kind: "step", desc: "Post to a Slack webhook" },
  { type: "ssh", name: "SSH", icon: "terminal", kind: "step", desc: "Run commands on a host" },
  { type: "if", name: "If", icon: "branch", kind: "step", desc: "Branch on a condition" },
  { type: "filter", name: "Filter", icon: "branch", kind: "step", desc: "Stop a branch unless condition holds" },
  { type: "merge", name: "Merge", icon: "branch", kind: "step", desc: "Join branches" },
  { type: "timegate", name: "Time gate", icon: "clock", kind: "step", desc: "Hold until a window" },
  { type: "wait", name: "Wait", icon: "wait", kind: "step", desc: "Pause for a duration" },
  { type: "approval", name: "Approval", icon: "approve", kind: "step", desc: "Require a human sign-off" },
  { type: "memory", name: "Memory write", icon: "db", kind: "step", desc: "Persist state" },
  { type: "status", name: "Status", icon: "flag", kind: "status", desc: "Mark run outcome" },
];

export const catalogByType = Object.fromEntries(CATALOG.map((c) => [c.type, c]));

// Status -> [badge class suffix, label].
export const STATUS_META: Record<Status, { cls: string; label: string }> = {
  pending: { cls: "idle", label: "Pending" },
  triggered: { cls: "wait", label: "Triggered" },
  waiting: { cls: "wait", label: "Waiting" },
  running: { cls: "run", label: "Running" },
  success: { cls: "ok", label: "Success" },
  created: { cls: "ok", label: "Created" },
  failed: { cls: "fail", label: "Failed" },
  skipped: { cls: "idle", label: "Skipped" },
  canceled: { cls: "idle", label: "Canceled" },
};

// Selectable edge labels (gates). "" means unconditional.
export const EDGE_LABELS = ["", "success", "failed", "approved"] as const;
