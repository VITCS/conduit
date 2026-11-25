"use client";
import { useEffect, useState } from "react";
import { Bot, Code2, KeyRound, CheckCircle2, AlertTriangle, Loader2, Plug, Terminal } from "lucide-react";
import { api, type ClaudeCheck, type ClaudeStatus } from "@/lib/api";

// How this project's coding steps (Plan changes / Apply changes) do their work.
// Everything here is stored as ordinary project variables and secrets — this tab
// is just a guided editor for them:
//   coding_engine            api | claude_code
//   claude_auth              subscription | api_key | server_login
//   claude_model, claude_allowed_commands, claude_timeout_minutes, claude_max_budget_usd
//   secrets: claude_code_oauth_token, anthropic_api_key

type Engine = "api" | "claude_code";
type Auth = "subscription" | "api_key" | "server_login";

const MODELS = [
  { id: "", label: "Claude Code default" },
  { id: "sonnet", label: "Sonnet (latest)" },
  { id: "opus", label: "Opus (latest)" },
  { id: "haiku", label: "Haiku (latest, fastest)" },
];

export function CodingAgentSettings(props: {
  vars: Record<string, string>;
  secrets: string[];
  onSaved: () => void;
  onToast: (m: string) => void;
}) {
  const v = props.vars;
  const [engine, setEngine] = useState<Engine>(v.coding_engine === "claude_code" ? "claude_code" : "api");
  const [auth, setAuth] = useState<Auth>((v.claude_auth as Auth) || (props.secrets.includes("anthropic_api_key") && !props.secrets.includes("claude_code_oauth_token") ? "api_key" : "subscription"));
  const [token, setToken] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState(v.claude_model ?? "");
  const [commands, setCommands] = useState(v.claude_allowed_commands ?? "");
  const [timeout, setTimeoutMin] = useState(v.claude_timeout_minutes ?? "");
  const [budget, setBudget] = useState(v.claude_max_budget_usd ?? "");
  const [status, setStatus] = useState<ClaudeStatus | null>(null);
  const [check, setCheck] = useState<ClaudeCheck | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "check">("");

  useEffect(() => {
    api.claudeStatus().then(setStatus).catch(() => setStatus(null));
  }, []);

  const hasToken = props.secrets.includes("claude_code_oauth_token");
  const hasKey = props.secrets.includes("anthropic_api_key");
  const customModel = model !== "" && !MODELS.some((m) => m.id === model);

  async function setOrClear(name: string, value: string) {
    if (value.trim()) await api.putVar(name, value.trim());
    else if (v[name] !== undefined) await api.deleteVar(name);
  }

  async function save(): Promise<boolean> {
    setBusy("save");
    try {
      await api.putVar("coding_engine", engine);
      if (engine === "claude_code") {
        await api.putVar("claude_auth", auth);
        if (auth === "subscription" && token.trim()) await api.putSecret("claude_code_oauth_token", token.trim());
        if (auth === "api_key" && apiKey.trim()) await api.putSecret("anthropic_api_key", apiKey.trim());
        await setOrClear("claude_model", model);
        await setOrClear("claude_allowed_commands", commands);
        await setOrClear("claude_timeout_minutes", timeout);
        await setOrClear("claude_max_budget_usd", auth === "api_key" ? budget : "");
      }
      setToken(""); setApiKey("");
      props.onToast("Coding agent saved");
      props.onSaved();
      return true;
    } catch (e) {
      props.onToast("Save failed: " + (e as Error).message);
      return false;
    } finally {
      setBusy("");
    }
  }

  async function test() {
    if (!(await save())) return;
    setBusy("check");
    setCheck(null);
    try {
      setCheck(await api.claudeCheck());
    } catch (e) {
      setCheck({ ok: false, error: (e as Error).message });
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="modal-body ca">
      <p className="hint" style={{ marginTop: -4 }}>
        How <b>Plan changes</b> and <b>Apply changes</b> write code for this project. Nothing reaches your
        repository before the file list is approved, whichever you choose.
      </p>

      <div className="ca-choice" role="radiogroup" aria-label="Coding engine">
        <button role="radio" aria-checked={engine === "claude_code"} className={`ca-opt${engine === "claude_code" ? " on" : ""}`}
          onClick={() => setEngine("claude_code")} data-testid="engine-claude">
          <span className="ca-ico"><Bot size={18} /></span>
          <span>
            <b>Claude Code</b> <em className="ca-tag">recommended</em>
            <small>Works in a real checkout: reads the whole repo, edits files and can run your tests.
              Uses your Claude subscription or an Anthropic API key.</small>
          </span>
        </button>
        <button role="radio" aria-checked={engine === "api"} className={`ca-opt${engine === "api" ? " on" : ""}`}
          onClick={() => setEngine("api")} data-testid="engine-api">
          <span className="ca-ico"><Code2 size={18} /></span>
          <span>
            <b>Direct API</b>
            <small>One model call per step and per file, through your <code>llm_provider</code> /
              <code> llm_model</code> / <code>llm_key</code>. Works with Anthropic, OpenAI or a gateway.</small>
          </span>
        </button>
      </div>

      {engine === "claude_code" && (
        <>
          {status && !status.installed && (
            <div className="ca-warn"><AlertTriangle size={15} /> The Claude Code CLI is not installed on this server.
              Use the default Docker image, or install <code>@anthropic-ai/claude-code</code>.</div>
          )}

          <div className="nt-field">
            <span className="nt-label">Sign in with</span>
            <div className="ca-auth" role="radiogroup" aria-label="Claude Code sign-in">
              <label className={auth === "subscription" ? "on" : ""}>
                <input type="radio" name="ca-auth" checked={auth === "subscription"} onChange={() => setAuth("subscription")} />
                Claude subscription
              </label>
              <label className={auth === "api_key" ? "on" : ""}>
                <input type="radio" name="ca-auth" checked={auth === "api_key"} onChange={() => setAuth("api_key")} />
                Anthropic API key
              </label>
              <label className={`${auth === "server_login" ? "on" : ""}${status?.server_login_allowed ? "" : " off"}`}
                title={status?.server_login_allowed ? "" : "The server operator has not enabled this (CONDUIT_CLAUDE_SERVER_LOGIN=true)"}>
                <input type="radio" name="ca-auth" checked={auth === "server_login"} disabled={!status?.server_login_allowed}
                  onChange={() => setAuth("server_login")} />
                This server&apos;s login
              </label>
            </div>
          </div>

          {auth === "subscription" && (
            <label className="nt-field">
              <span className="nt-label">Subscription token {hasToken && <span className="ca-saved"><KeyRound size={11} /> saved</span>}</span>
              <input className="inp mono" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)}
                placeholder={hasToken ? "leave empty to keep the saved token" : "sk-ant-oat01-…"} data-testid="claude-token" />
              <span className="hint">On a computer signed in to your Claude plan, run <code>claude setup-token</code> and paste the
                token. Runs use your plan&apos;s usage limits instead of API credits. Stored encrypted.</span>
            </label>
          )}
          {auth === "api_key" && (
            <label className="nt-field">
              <span className="nt-label">Anthropic API key {hasKey && <span className="ca-saved"><KeyRound size={11} /> saved</span>}</span>
              <input className="inp mono" type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
                placeholder={hasKey ? "leave empty to keep the saved key" : "sk-ant-api03-…"} />
              <span className="hint">Billed per use to your Anthropic account. Stored encrypted. If empty, an Anthropic
                <code> llm_key</code> is used.</span>
            </label>
          )}

          <div className="nt-row">
            <label className="nt-field">
              <span className="nt-label">Model</span>
              <select className="inp" value={customModel ? "__custom" : model} onChange={(e) => setModel(e.target.value === "__custom" ? "claude-" : e.target.value)}>
                {MODELS.map((m) => <option key={m.id} value={m.id}>{m.label}</option>)}
                <option value="__custom">Specific model ID…</option>
              </select>
              {customModel && <input className="inp mono" value={model} onChange={(e) => setModel(e.target.value)} placeholder="model ID" />}
            </label>
            <label className="nt-field">
              <span className="nt-label">Time limit <span className="nt-opt">minutes</span></span>
              <input className="inp mono" inputMode="numeric" value={timeout} onChange={(e) => setTimeoutMin(e.target.value.replace(/[^0-9]/g, ""))} placeholder="20" />
            </label>
            {auth === "api_key" && (
              <label className="nt-field">
                <span className="nt-label">Max spend per run <span className="nt-opt">USD</span></span>
                <input className="inp mono" inputMode="decimal" value={budget} onChange={(e) => setBudget(e.target.value.replace(/[^0-9.]/g, ""))} placeholder="no limit" />
              </label>
            )}
          </div>

          <label className="nt-field">
            <span className="nt-label"><Terminal size={12} style={{ verticalAlign: -1 }} /> Commands it may run <span className="nt-opt">optional, comma-separated</span></span>
            <input className="inp mono" value={commands} onChange={(e) => setCommands(e.target.value)} placeholder="npm ci, npm test" data-testid="claude-commands" />
            <span className="hint">Leave empty for no shell at all (CI still tests the branch). Each entry allows that command
              and its arguments, e.g. <code>npm test</code> or <code>go test</code>. <code>git push</code> is never allowed:
              Conduit pushes only after checking the approved files.</span>
          </label>
        </>
      )}

      {check && (
        <div className={`ca-result ${check.ok ? "ok" : "bad"}`} data-testid="claude-check">
          {check.ok ? <CheckCircle2 size={16} /> : <AlertTriangle size={16} />}
          <span>
            {check.ok
              ? <>Connected{check.version ? <> · {check.version}</> : null} · signed in with {authLabel(check.auth)}</>
              : check.error}
          </span>
        </div>
      )}

      <div className="nt-actions">
        {engine === "claude_code" && (
          <button className="btn" onClick={test} disabled={busy !== ""} data-testid="claude-test">
            {busy === "check" ? <Loader2 size={14} className="spin" /> : <Plug size={14} />} Save &amp; test connection
          </button>
        )}
        <button className="btn primary" onClick={save} disabled={busy !== ""} data-testid="coding-save">
          {busy === "save" ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

function authLabel(a?: string) {
  return a === "subscription" ? "your Claude subscription" : a === "api_key" ? "an Anthropic API key" : a === "server_login" ? "the server's login" : a || "—";
}
