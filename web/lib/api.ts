import type { Run, Workflow } from "./types";

// Same-origin by default (the Go server serves both UI and API). For `next dev`
// against a separately-running server, set NEXT_PUBLIC_API_BASE.
const BASE = process.env.NEXT_PUBLIC_API_BASE ?? "";

const TOKEN_KEY = "conduit_token";

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) || "";
  } catch {
    return "";
  }
}
export function setToken(t: string) {
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* ignore */
  }
}

// onUnauthorized is invoked when the API returns 401 so the app can show login.
export let onUnauthorized: () => void = () => {};
export function setOnUnauthorized(fn: () => void) {
  onUnauthorized = fn;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const tok = getToken();
  if (tok) headers.set("Authorization", "Bearer " + tok);
  const r = await fetch(BASE + path, { ...init, headers });
  if (r.status === 401) {
    setToken("");
    onUnauthorized();
  }
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
  login: (email: string, password: string) =>
    req<{ token: string; user: { email: string; name: string; role: string } }>("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    }),
  me: () => req<{ email: string; name: string; role: string }>("/api/auth/me"),
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
  listRuns: () => req<Run[]>("/api/runs"),
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
  listSecrets: () => req<string[]>("/api/secrets"),
  putSecret: (name: string, value: string) =>
    req<{ ok: boolean }>(`/api/secrets/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ value }),
    }),
  deleteSecret: (name: string) =>
    req<{ ok: boolean }>(`/api/secrets/${encodeURIComponent(name)}`, { method: "DELETE" }),
};
