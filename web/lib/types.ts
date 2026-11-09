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
}

// Data carried on a React Flow edge.
export interface EdgeData {
  label: string;
  maxAttempts?: number; // mirrors WFEdge.max_attempts for a retry back-edge
  [key: string]: unknown;
}
