// Domain types mirroring the Go API (internal/model).

export type Status =
  | "pending"
  | "triggered"
  | "running"
  | "waiting"
  | "success"
  | "created"
  | "failed"
  | "skipped"
  | "canceled";

export type Kind = "trigger" | "step" | "status";

export interface Field {
  key: string;
  icon: string;
  value: string;
}

export interface WFNode {
  id: string;
  type: string;
  title: string;
  icon: string;
  kind: Kind;
  x: number;
  y: number;
  config: Field[];
  disabled?: boolean; // bypass this step during runs
}

export interface WFEdge {
  from: string;
  to: string;
  label?: string;
  max_attempts?: number; // > 0 marks a bounded retry back-edge
}

export interface Workflow {
  id: string;
  project_id?: string;
  name: string;
  nodes: WFNode[];
  edges: WFEdge[];
}

export interface StepRun {
  node_id: string;
  status: Status;
  started_at?: string;
  ended_at?: string;
  output?: Record<string, unknown>;
  logs?: string[];
  usage?: Usage;
}

/** Model token usage and cost, for a step or summed over a run. */
export interface Usage {
  model?: string;
  calls: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  /** Billed at API prices. */
  cost_usd: number;
  /** API-price equivalent of subscription work (not billed per token). */
  subscription_usd?: number;
  unpriced?: number;
  by_model?: Record<string, Usage>;
}

export interface Run {
  id: string;
  workflow_id: string;
  title: string;
  status: Status;
  created_at: string;
  finished_at?: string;
  steps: Record<string, StepRun>;
  memory: Record<string, unknown>;
  input?: Record<string, unknown>;
  usage?: Usage;
}

export interface SSEvent {
  type: string;
  run_id: string;
  node_id?: string;
  status?: Status;
  level?: string;
  message?: string;
  detail?: string;
  step?: StepRun;
  run?: Run;
  task?: Task;
  review?: ReviewRequest;
  project_id?: string;
  time: string;
}

// Data carried on a React Flow node.
export interface NodeData {
  title: string;
  typeName: string;
  icon: string;
  kind: Kind;
  config: Field[];
  status: Status;
  [key: string]: unknown;
}

export interface ReviewRequest {
  id: string;
  project_id: string;
  workflow_id: string;
  owner: string;
  repo: string;
  number: number;
  url: string;
  head?: string;
  title?: string;
  source?: string;
  source_run?: string;
  status: string;
  run_id?: string;
  created_at: string;
}

export type TaskStage = "backlog" | "implement" | "verify" | "done" | "blocked";

export interface Task {
  id: string;
  project_id: string;
  workflow_id?: string;
  title: string;
  source?: string;
  source_ref?: string;
  source_url?: string;
  summary?: string;
  description?: string;
  stage: TaskStage;
  confidence?: number;
  current_run_id?: string;
  branch?: string;
  pr_url?: string;
  created_at: string;
  updated_at: string;
  stage_changed_at: string;
  history?: { stage: TaskStage; at: string }[];
  notes?: { author?: string; text: string; at: string }[];
}

export interface BoardMetrics {
  tasks: number;
  by_stage: Record<string, number>;
  done: number;
  blocked: number;
  started: number;
  avg_lead_ms: number;
  stage_avg_ms: Record<string, number>;
  stage_inflow: Record<string, number>;
  runs: number;
  retry_runs: number;
  retry_rate: number;
  intervention_pct: number;
  usage?: {
    cost_usd: number;
    subscription_usd: number;
    input_tokens: number;
    output_tokens: number;
    calls: number;
    unpriced: number;
    runs_with_usage: number;
    avg_run_cost_usd: number;
    by_model: Usage[];
  };
}

// Data carried on a React Flow edge.
export interface EdgeData {
  label: string;
  maxAttempts?: number; // mirrors WFEdge.max_attempts for a retry back-edge
  [key: string]: unknown;
}

/** One thing waiting on a person ("Needs you"). */
export interface AttentionItem {
  kind: "question" | "approval" | "stuck" | "review";
  project_id: string;
  project_name: string;
  run_id?: string;
  node_id?: string;
  task_id?: string;
  review_id?: string;
  title: string;
  step?: string;
  text?: string;
  options?: string[];
  /** For a stuck item: stalled (no progress), not_started (queued, never picked up) or blocked (a blocked task). */
  reason?: "stalled" | "not_started" | "blocked";
  url?: string;
  since: string;
}

export interface Attention {
  items: AttentionItem[];
  by_project: Record<string, number>;
  total: number;
}
