import type { Run, Workflow, Task, BoardMetrics, ReviewRequest } from "./types";

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

const PROJECT_KEY = "conduit_project";
export function getProject(): string {
  try {
    return localStorage.getItem(PROJECT_KEY) || "default";
  } catch {
    return "default";
  }
}
export function setProject(p: string) {
  try {
    localStorage.setItem(PROJECT_KEY, p);
  } catch {
    /* ignore */
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const tok = getToken();
  if (tok) headers.set("Authorization", "Bearer " + tok);
  headers.set("X-Conduit-Project", getProject());
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
  register: (email: string, name: string, password: string) =>
    req<{
      token: string;
      user: { email: string; name: string; role: string };
      project: { id: string; name: string };
    }>("/api/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, name, password }),
    }),
  authConfig: () =>
    req<{ signup_enabled: boolean; methods: string[]; providers: string[] }>("/api/auth/config"),
  // Full-page redirect into the provider's consent screen (OAuth can't be an XHR).
  oauthStartURL: (provider: string) => `${BASE}/api/auth/oauth/${encodeURIComponent(provider)}/start`,
  me: () => req<{ email: string; name: string; role: string }>("/api/auth/me"),
  listProjects: () => req<{ id: string; name: string }[]>("/api/projects"),
  createProject: (name: string) =>
    req<{ id: string; name: string }>("/api/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    }),
  deleteProject: (id: string) =>
    req<{ ok: boolean }>(`/api/projects/${encodeURIComponent(id)}`, { method: "DELETE" }),
  listMembers: (projectId: string) =>
    req<{ user_id: string; email: string; name: string; role: string }[]>(
      `/api/projects/${encodeURIComponent(projectId)}/members`,
    ),
  addMember: (projectId: string, email: string, role: string) =>
    req<unknown>(`/api/projects/${encodeURIComponent(projectId)}/members`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, role }),
    }),
  removeMember: (projectId: string, userId: string) =>
    req<unknown>(`/api/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`, {
      method: "DELETE",
    }),
  createUser: (email: string, name: string, password: string, role: string) =>
    req<unknown>("/api/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email, name, password, role }),
    }),
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
  rerun: (runId: string, nodeId: string) =>
    req<Run>(`/api/runs/${encodeURIComponent(runId)}/rerun`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ node_id: nodeId }),
    }),
  listReviewRequests: () => req<ReviewRequest[]>("/api/review-requests?status=pending"),
  runReviewRequest: (id: string) =>
    req<{ ok: boolean; run: Run }>(`/api/review-requests/${encodeURIComponent(id)}/run`, { method: "POST" }),
  dismissReviewRequest: (id: string) =>
    req<{ ok: boolean }>(`/api/review-requests/${encodeURIComponent(id)}/dismiss`, { method: "POST" }),
  // EventSource cannot send an Authorization header, so the token rides along as a
  // same-origin query parameter (the server accepts it only for this endpoint).
  eventsURL: (runId: string) =>
    `${BASE}/api/events?run=${encodeURIComponent(runId)}&access_token=${encodeURIComponent(getToken())}`,
  // Project-wide stream (no run filter) — used by the board for task.* events.
  eventsAllURL: () => `${BASE}/api/events?access_token=${encodeURIComponent(getToken())}`,

  listTasks: () => req<Task[]>("/api/tasks"),
  getTask: (id: string) => req<{ task: Task; runs: Run[] }>(`/api/tasks/${encodeURIComponent(id)}`),
  createTask: (body: Partial<Task>) =>
    req<Task>("/api/tasks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
  startTask: (id: string) =>
    req<{ task: Task; run: Run }>(`/api/tasks/${encodeURIComponent(id)}/start`, { method: "POST" }),
  setTaskStage: (id: string, stage: string) =>
    req<Task>(`/api/tasks/${encodeURIComponent(id)}/stage`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ stage }),
    }),
  deleteTask: (id: string) =>
    req<void>(`/api/tasks/${encodeURIComponent(id)}`, { method: "DELETE" }),
  addTaskNote: (id: string, text: string) =>
    req<Task>(`/api/tasks/${encodeURIComponent(id)}/notes`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text }),
    }),
  getMetrics: () => req<BoardMetrics>("/api/metrics"),
  listSecrets: () => req<string[]>("/api/secrets"),
  putSecret: (name: string, value: string) =>
    req<{ ok: boolean }>(`/api/secrets/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ value }),
    }),
  deleteSecret: (name: string) =>
    req<{ ok: boolean }>(`/api/secrets/${encodeURIComponent(name)}`, { method: "DELETE" }),
  // Project config variables (${var:NAME}) — plain, non-secret, set once per project.
  listVars: () => req<Record<string, string>>("/api/vars"),
  putVar: (name: string, value: string) =>
    req<{ ok: boolean }>(`/api/vars/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ value }),
    }),
  deleteVar: (name: string) =>
    req<{ ok: boolean }>(`/api/vars/${encodeURIComponent(name)}`, { method: "DELETE" }),
};
