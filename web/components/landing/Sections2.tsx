"use client";
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { ArrowRight, Check, ChevronDown, ChevronRight, MessageSquare, TrendingUp } from "lucide-react";
import {
  CONTACT_URL, EXEC_SERIES, FAQ, KPIS, LIVE_FEED, PRICING, PRICING_COMPARE, TIME_SAVED, USE_CASES,
} from "./data";
import { CountUp, DemoTag, EASE, Reveal, SectionHead } from "./ui";

/* -------------------------------------------------------------- use cases */

export function UseCases() {
  const [active, setActive] = useState(USE_CASES[0].id);
  const uc = USE_CASES.find((u) => u.id === active)!;
  return (
    <section className="cv-sec" id="usecases">
      <SectionHead kicker="Use cases" title="Automation for every team." />
      <Reveal>
        <div className="cv-tabs" role="tablist" aria-label="Teams">
          {USE_CASES.map((u) => (
            <button key={u.id} role="tab" id={`uc-tab-${u.id}`} aria-selected={active === u.id}
              aria-controls="uc-panel" className={active === u.id ? "on" : ""} onClick={() => setActive(u.id)}>
              {u.tab}
              {active === u.id && <motion.span layoutId="cvTabPill" className="pill" transition={{ duration: 0.3, ease: EASE }} />}
            </button>
          ))}
        </div>
        <div className="cv-uc" role="tabpanel" id="uc-panel" aria-labelledby={`uc-tab-${uc.id}`}>
          <AnimatePresence mode="wait">
            <motion.div key={uc.id} className="cv-uc-in"
              initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.28, ease: EASE }}>
              <div className="cv-uc-copy">
                <h3>{uc.title}</h3>
                <p>{uc.body}</p>
                <div className="cv-uc-metric">
                  <TrendingUp size={16} />
                  <div><b>{uc.metric}</b><span>{uc.metricLabel}</span></div>
                  <DemoTag>Illustrative</DemoTag>
                </div>
              </div>
              <div className="cv-uc-flow" aria-label={`Workflow: ${uc.flow.join(", then ")}`}>
                {uc.flow.map((s, i) => (
                  <motion.div key={s} className="cv-uc-step"
                    initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.3, delay: 0.12 + i * 0.09 }}>
                    <span className={`cv-mini-node ${i === 0 ? "c" : /AI|agent|Triage|Plan/i.test(s) ? "v" : /^IF|Approve|Approval|Else/i.test(s) ? "g" : ""}`}>
                      <i>{String(i + 1).padStart(2, "0")}</i>{s}
                    </span>
                    {i < uc.flow.length - 1 && <ChevronRight size={14} className="arr" />}
                  </motion.div>
                ))}
              </div>
            </motion.div>
          </AnimatePresence>
        </div>
      </Reveal>
    </section>
  );
}

/* -------------------------------------------------------------- analytics */

const tipStyle = {
  background: "var(--cv-s1)", border: "1px solid var(--cv-line2)", borderRadius: 10,
  color: "var(--cv-fg)", fontSize: 12, boxShadow: "0 10px 30px rgba(0,0,0,.35)",
};

function LiveFeed({ on }: { on: boolean }) {
  const reduced = useReducedMotion();
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!on || reduced) return;
    const id = window.setInterval(() => setTick((t) => t + 1), 2400);
    return () => window.clearInterval(id);
  }, [on, reduced]);
  const rows = Array.from({ length: 5 }, (_, i) => {
    const n = tick + 4 - i; // newest first
    return { n, ...LIVE_FEED[n % LIVE_FEED.length] };
  });
  return (
    <ul className="cv-feed">
      <AnimatePresence initial={false}>
        {rows.map((r) => (
          <motion.li key={r.n} layout className={r.status}
            initial={{ opacity: 0, y: -14 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}>
            <i /><span className="wf">{r.wf}</span>
            <span className="id">#{2300 + r.n}</span>
            <span className="st">{r.status === "ok" ? `${(r.ms / 1000).toFixed(1)}s` : r.status === "wait" ? "awaiting approval" : "failed · retried"}</span>
          </motion.li>
        ))}
      </AnimatePresence>
    </ul>
  );
}

export function Analytics() {
  const ref = useRef<HTMLDivElement>(null);
  const seen = useInView(ref, { once: true, margin: "-120px" });
  const live = useInView(ref, { margin: "-80px" });
  return (
    <section className="cv-sec" id="analytics">
      <SectionHead
        kicker="Analytics & monitoring"
        title="Know what ran, what failed and what it saved."
        sub="A live view of every workflow: executions, outcomes, timings and history."
      />
      <Reveal>
        <div className="cv-dash" ref={ref}>
          <div className="cv-dash-bar">
            <span className="cv-dots" aria-hidden><i /><i /><i /></span>
            <b>Workspace overview</b><span className="range">Last 14 days</span>
            <DemoTag />
            <span className="cv-stage-status run"><i /> live</span>
          </div>
          <div className="cv-kpis">
            {KPIS.map((k) => (
              <div key={k.label} className={`cv-kpi t-${k.tone}`}>
                <span>{k.label}</span>
                <b><CountUp value={k.value} decimals={k.decimals} suffix={k.suffix} /></b>
              </div>
            ))}
          </div>
          <div className="cv-dash-grid">
            <div className="cv-dash-card big">
              <h4>Executions <small>successful vs failed</small></h4>
              <div className="cv-chart">
                {seen && (
                  <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={EXEC_SERIES} margin={{ top: 8, right: 8, left: -18, bottom: 0 }}>
                      <defs>
                        <linearGradient id="cvOk" x1="0" y1="0" x2="0" y2="1">
                          <stop offset="0" stopColor="#8B5CF6" stopOpacity={0.45} />
                          <stop offset="1" stopColor="#8B5CF6" stopOpacity={0} />
                        </linearGradient>
                      </defs>
                      <CartesianGrid stroke="var(--cv-line)" vertical={false} />
                      <XAxis dataKey="d" tick={{ fill: "var(--cv-faint)", fontSize: 11 }} axisLine={false} tickLine={false} />
                      <YAxis tick={{ fill: "var(--cv-faint)", fontSize: 11 }} axisLine={false} tickLine={false} />
                      <Tooltip contentStyle={tipStyle} cursor={{ stroke: "var(--cv-line2)" }} />
                      <Area type="monotone" dataKey="ok" name="Successful" stroke="#8B5CF6" strokeWidth={2} fill="url(#cvOk)" animationDuration={1200} />
                      <Area type="monotone" dataKey="fail" name="Failed" stroke="#F87171" strokeWidth={1.6} fill="transparent" animationDuration={1200} />
                    </AreaChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>
            <div className="cv-dash-card">
              <h4>Time saved <small>hours by team</small></h4>
              <div className="cv-chart">
                {seen && (
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={TIME_SAVED} margin={{ top: 8, right: 4, left: -22, bottom: 0 }}>
                      <CartesianGrid stroke="var(--cv-line)" vertical={false} />
                      <XAxis dataKey="team" tick={{ fill: "var(--cv-faint)", fontSize: 11 }} axisLine={false} tickLine={false} />
                      <YAxis tick={{ fill: "var(--cv-faint)", fontSize: 11 }} axisLine={false} tickLine={false} />
                      <Tooltip contentStyle={tipStyle} cursor={{ fill: "var(--cv-line)" }} />
                      <Bar dataKey="h" name="Hours" fill="#22D3EE" radius={[5, 5, 0, 0]} animationDuration={1000} />
                    </BarChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>
            <div className="cv-dash-card wide">
              <h4>Execution history <small>most recent first</small></h4>
              <LiveFeed on={live} />
            </div>
          </div>
        </div>
      </Reveal>
    </section>
  );
}

/* ---------------------------------------------------------------- pricing */

export function Pricing({ onGetStarted }: { onGetStarted: () => void }) {
  return (
    <section className="cv-sec" id="pricing">
      <SectionHead kicker="Pricing" title="Start small. Scale when it works." />
      <div className="cv-prices">
        {PRICING.map((p, i) => (
          <Reveal key={p.id} delay={i * 0.08} className={`cv-price${p.featured ? " featured" : ""}`}>
            {p.featured && <span className="cv-price-flag">Most popular</span>}
            <h3>{p.name}</h3>
            <p className="blurb">{p.blurb}</p>
            <div className="amt">
              <b>{p.price ?? "Custom"}</b>{p.price && <span>{p.period}</span>}
            </div>
            {p.cta === "start" ? (
              <button className={`cv-btn ${p.featured ? "primary" : "ghost"} block`} onClick={onGetStarted}>Start Building Free</button>
            ) : (
              <a className="cv-btn ghost block" href={CONTACT_URL} target="_blank" rel="noreferrer">Talk to Our Team</a>
            )}
            <ul>
              {p.features.map((f) => <li key={f}><Check size={14} />{f}</li>)}
            </ul>
          </Reveal>
        ))}
      </div>
      <Reveal>
        <div className="cv-compare" role="region" aria-label="Plan comparison" tabIndex={0}>
          <table>
            <thead><tr><th scope="col">Compare</th>{PRICING.map((p) => <th scope="col" key={p.id}>{p.name}</th>)}</tr></thead>
            <tbody>
              {PRICING_COMPARE.map((r) => (
                <tr key={r.feature}><th scope="row">{r.feature}</th>{r.values.map((v, i) => <td key={i}>{v}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="cv-note">Prices and plan limits shown are placeholders. Conduit itself is open source and self-hosted.</p>
      </Reveal>
    </section>
  );
}

/* -------------------------------------------------------------------- faq */

export function Faq() {
  const [open, setOpen] = useState<number | null>(0);
  return (
    <section className="cv-sec narrow" id="faq">
      <SectionHead kicker="FAQ" title="Questions, answered." />
      <div className="cv-faq">
        {FAQ.map((f, i) => {
          const isOpen = open === i;
          return (
            <Reveal key={f.q} delay={Math.min(i, 4) * 0.04}>
              <div className={`cv-faq-item${isOpen ? " open" : ""}`}>
                <h3>
                  <button aria-expanded={isOpen} aria-controls={`faq-a-${i}`} id={`faq-q-${i}`}
                    onClick={() => setOpen(isOpen ? null : i)}>
                    {f.q}<ChevronDown size={18} />
                  </button>
                </h3>
                <AnimatePresence initial={false}>
                  {isOpen && (
                    <motion.div id={`faq-a-${i}`} role="region" aria-labelledby={`faq-q-${i}`}
                      initial={{ height: 0, opacity: 0 }} animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }} transition={{ duration: 0.28, ease: EASE }}
                      style={{ overflow: "hidden" }}>
                      <p>{f.a}</p>
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            </Reveal>
          );
        })}
      </div>
    </section>
  );
}

/* -------------------------------------------------------------- final cta */

// A fixed constellation: deterministic so server and client render the same.
const NET: [number, number][] = [
  [60, 70], [190, 40], [330, 96], [470, 44], [620, 88], [760, 36], [900, 92], [1040, 50], [1150, 110],
  [110, 210], [270, 250], [430, 200], [590, 262], [750, 210], [910, 258], [1080, 214],
  [50, 350], [210, 388], [380, 342], [540, 392], [700, 348], [860, 392], [1020, 350], [1160, 380],
];
const LINKS: [number, number][] = [
  [0, 1], [1, 2], [2, 3], [3, 4], [4, 5], [5, 6], [6, 7], [7, 8], [0, 9], [1, 10], [2, 10], [2, 11], [3, 11],
  [4, 12], [5, 13], [6, 14], [7, 15], [8, 15], [9, 10], [10, 11], [11, 12], [12, 13], [13, 14], [14, 15],
  [9, 16], [10, 17], [11, 18], [12, 19], [13, 20], [14, 21], [15, 22], [15, 23], [16, 17], [17, 18],
  [18, 19], [19, 20], [20, 21], [21, 22], [22, 23],
];

export function FinalCta({ onGetStarted }: { onGetStarted: () => void }) {
  return (
    <section className="cv-final" id="start">
      <svg className="cv-net" viewBox="0 0 1200 430" preserveAspectRatio="xMidYMid slice" aria-hidden>
        {LINKS.map(([a, b], i) => (
          <line key={i} x1={NET[a][0]} y1={NET[a][1]} x2={NET[b][0]} y2={NET[b][1]} className={i % 5 === 0 ? "flow" : ""} />
        ))}
        {NET.map(([x, y], i) => <circle key={i} cx={x} cy={y} r={i % 4 === 0 ? 3.2 : 2} className={i % 3 === 0 ? "p v" : "p"} style={{ animationDelay: `${(i % 7) * 0.6}s` }} />)}
      </svg>
      <Reveal className="cv-final-in">
        <h2>Stop managing tasks.<br /><span className="cv-grad">Start orchestrating intelligence.</span></h2>
        <p>Your next level of productivity starts with one workflow.</p>
        <div className="cv-cta-row ctr">
          <button className="cv-btn primary lg" onClick={onGetStarted}>Start Building Free <ArrowRight size={17} /></button>
          <a className="cv-btn ghost lg" href={CONTACT_URL} target="_blank" rel="noreferrer"><MessageSquare size={16} /> Talk to Our Team</a>
        </div>
      </Reveal>
    </section>
  );
}
