"use client";
import { useState } from "react";
import { Gauge, Wand2 } from "lucide-react";
import { api } from "@/lib/api";

// Model tiers and automatic step-up. Stored as project variables:
//   model_fast, model_standard, model_max   — the model for each tier
//   model_escalation                        — "off" disables step-up
//   escalate_confidence_below, escalate_files_over — step-up thresholds
//   model_prices                            — prices for models Conduit doesn't know
// With no tier set, every step keeps using llm_model (or claude_model).

// First-party Claude API list prices, USD per million tokens (input / output).
// Mirrors the server's table, which is what costs are computed from.
export const MODEL_PRICES: Record<string, [number, number]> = {
  "claude-haiku-5-5": [0.1, 0.5],
  "claude-sonnet-5-5": [2, 10],
  "claude-opus-5-5": [4, 20],
  "claude-fable-5-2": [8, 40],
};

const PRESET = { model_fast: "claude-haiku-5-5", model_standard: "claude-sonnet-5-5", model_max: "claude-opus-5-5" };

const TIERS: { key: keyof typeof PRESET; label: string; uses: string }[] = [
  { key: "model_fast", label: "Fast", uses: "Triage and the factory's Review Confidence step" },
  { key: "model_standard", label: "Standard", uses: "Planning, drafting, coding and PR review" },
  { key: "model_max", label: "Best", uses: "Step-ups: hard tickets, large changes, retries" },
];

const KEYS = ["model_fast", "model_standard", "model_max", "model_escalation", "escalate_confidence_below", "escalate_files_over", "model_prices"] as const;
type Key = (typeof KEYS)[number];

function priceLabel(m: string) {
  const p = MODEL_PRICES[m.trim()];
  return p ? `$${p[0].toFixed(2)} / $${p[1].toFixed(2)} per M tokens` : m.trim() ? "price from model_prices, if set" : "";
}

export function ModelSettings(props: { vars: Record<string, string>; onSaved: () => void; onToast: (m: string) => void }) {
  const initial = Object.fromEntries(KEYS.map((k) => [k, props.vars[k] ?? ""])) as Record<Key, string>;
  const [v, setV] = useState<Record<Key, string>>(initial);
  const [busy, setBusy] = useState(false);
  const set = (k: Key, val: string) => setV((s) => ({ ...s, [k]: val }));
  const dirty = KEYS.some((k) => v[k].trim() !== initial[k].trim());
  const tiersOn = !!(v.model_fast.trim() || v.model_standard.trim() || v.model_max.trim());
  const stepUp = v.model_escalation.trim().toLowerCase() !== "off";

  async function save() {
    setBusy(true);
    try {
      for (const k of KEYS) {
        const val = v[k].trim();
        if (val === initial[k].trim()) continue;
        if (val) await api.putVar(k, val);
        else await api.deleteVar(k);
      }
      props.onToast("Model settings saved");
      props.onSaved();
    } catch (e) {
      props.onToast("Save failed: " + (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-body">
      <p className="hint" style={{ marginTop: -4 }}>
        Use a cheaper model where it is enough and the strongest one where it matters. Each step uses its tier;
        a step set to a specific model keeps it. With no tiers set, every step uses <code>llm_model</code>
        {" "}(Claude Code: <code>claude_model</code>) as before. Use full model IDs; they work for the API and for Claude Code.
      </p>

      <datalist id="mc-models">
        {Object.keys(MODEL_PRICES).map((m) => <option key={m} value={m} />)}
      </datalist>

      <div className="mc-tiers">
        {TIERS.map((t) => (
          <label key={t.key} className="nt-field mc-tier">
            <span className="nt-label">{t.label} <span className="nt-opt">{t.uses}</span></span>
            <input className="inp" list="mc-models" value={v[t.key]} placeholder={PRESET[t.key]}
              onChange={(e) => set(t.key, e.target.value)} data-testid={`tier-${t.key}`} />
            <small className="mc-price">{priceLabel(v[t.key])}</small>
          </label>
        ))}
      </div>
      <button className="linkbtn" onClick={() => setV((s) => ({ ...s, ...PRESET }))} data-testid="tier-preset">
        <Wand2 size={13} /> Use Claude Haiku / Sonnet / Opus
      </button>

      <div className="mc-box">
        <label className="mc-switch">
          <input type="checkbox" checked={stepUp} disabled={!tiersOn}
            onChange={(e) => set("model_escalation", e.target.checked ? "" : "off")} data-testid="tier-escalation" />
          <span><Gauge size={13} /> <b>Step up automatically</b></span>
        </label>
        <ul className="mc-rules">
          <li>Triage confidence below
            <input className="inp mc-num" inputMode="numeric" value={v.escalate_confidence_below} placeholder="75"
              onChange={(e) => set("escalate_confidence_below", e.target.value.replace(/\D/g, ""))} disabled={!stepUp || !tiersOn} />
            , or a change plan with more than
            <input className="inp mc-num" inputMode="numeric" value={v.escalate_files_over} placeholder="5"
              onChange={(e) => set("escalate_files_over", e.target.value.replace(/\D/g, ""))} disabled={!stepUp || !tiersOn} />
            files: coding steps use the <b>Best</b> model.</li>
          <li>A retry after failing CI, or a step sent back with feedback: one tier up.</li>
          <li>The run log says which model each step used and why.</li>
        </ul>
      </div>

      <label className="nt-field">
        <span className="nt-label">Other model prices <span className="nt-opt">optional · USD per million tokens, input/output</span></span>
        <textarea className="inp nt-area" rows={2} value={v.model_prices} onChange={(e) => set("model_prices", e.target.value)}
          placeholder="gpt-4o-mini=0.15/0.60, my-gateway-model=1/3" />
      </label>
      <p className="hint">
        Claude prices are built in. Costs are estimates from the tokens each provider reports; with a Claude
        subscription, Claude Code work is shown as the API-price equivalent and is not billed per token.
      </p>

      <div className="nt-actions">
        <button className="btn primary" onClick={save} disabled={busy || !dirty} data-testid="tier-save">
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}
