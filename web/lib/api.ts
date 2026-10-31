import type { Run, Workflow } from "./types";

// Same-origin by default (the Go server serves both UI and API). For `next dev`
// against a separately-running server, set NEXT_PUBLIC_API_BASE.
const BASE = process.env.NEXT_PUBLIC_API_BASE ?? "";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(BASE + path, init);
  if (!r.ok) {
    let msg = r.statusText;
    try {
      const body = await r.json();
      msg = body.error || msg;
      // Validation responses carry a details[] list — surface it to the user.
      if (Array.isArray(body.details) && body.details.length) {
        msg += ": " + body.details.join("; ");
      }
    } catch {
      /* ignore */
    }
    throw new Error(msg);
  }
  return (await r.json()) as T;
}

export const api = {
  listWorkflows: () => req<Workflow[]>("/api/workflows"),
  getWorkflow: (id: string) => req<Workflow>(`/api/workflows/${encodeURIComponent(id)}`),
  putWorkflow: (wf: Workflow) =>
    req<Workflow>(`/api/workflows/${encodeURIComponent(wf.id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(wf),
    }),
  createRun: (workflowId: string) =>
    req<Run>("/api/runs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ workflow_id: workflowId }),
    }),
  getRun: (id: string) => req<Run>(`/api/runs/${encodeURIComponent(id)}`),
  approve: (runId: string, nodeId: string, decision: "approved" | "rejected") =>
    req<{ ok: boolean }>(`/api/runs/${encodeURIComponent(runId)}/approve`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ node_id: nodeId, decision }),
    }),
  cancelRun: (runId: string) =>
    req<{ ok: boolean }>(`/api/runs/${encodeURIComponent(runId)}/cancel`, { method: "POST" }),
  eventsURL: (runId: string) => `${BASE}/api/events?run=${encodeURIComponent(runId)}`,
};
