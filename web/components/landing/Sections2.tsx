"use client";
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion, useInView, useReducedMotion } from "framer-motion";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { ArrowRight, BookOpen, Check, ChevronDown, ChevronRight, Github, ShieldCheck } from "lucide-react";
import { BY_STAGE, DOCS_URL, FAQ, KPIS, REPO_URL, RUN_FEED, STAGE_DWELL, WORKFLOWS } from "./data";
import { CountUp, DemoTag, EASE, Reveal, SectionHead } from "./ui";

/* -------------------------------------------------------------- workflows */

export function Workflows() {
  const [active, setActive] = useState(WORKFLOWS[0].id);
  const wf = WORKFLOWS.find((u) => u.id === active)!;
  return (
    <section className="cv-sec" id="workflows">
      <SectionHead kicker="Workflows" title="Two workflows ship ready. The canvas builds the rest." />
      <Reveal>
        <div className="cv-tabs" role="tablist" aria-label="Workflows">
          {WORKFLOWS.map((u) => (
            <button key={u.id} role="tab" id={`wf-tab-${u.id}`} aria-selected={active === u.id}
              aria-controls="wf-panel" className={active === u.id ? "on" : ""} onClick={() => setActive(u.id)}>
              {u.tab}
              {active === u.id && <motion.span layoutId="cvTabPill" className="pill" transition={{ duration: 0.3, ease: EASE }} />}
            </button>
          ))}
        </div>
        <div className="cv-uc" role="tabpanel" id="wf-panel" aria-labelledby={`wf-tab-${wf.id}`}>
          <AnimatePresence mode="wait">
            <motion.div key={wf.id} className="cv-uc-in"
              initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}
              transition={{ duration: 0.28, ease: EASE }}>
              <div className="cv-uc-copy">
                <h3>{wf.title}</h3>
                <p>{wf.body}</p>
                <div className="cv-uc-metric">
                  <ShieldCheck size={16} />
                  <div><b>{wf.fact}</b><span>{wf.factLabel}</span></div>
                </div>
              </div>
              <div className="cv-uc-flow" aria-label={`Workflow: ${wf.flow.join(", then ")}`}>
                {wf.flow.map((s, i) => (
                  <motion.div key={`${s}-${i}`} className="cv-uc-step"
                    initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.3, delay: 0.12 + i * 0.07 }}>
                    <span className={`cv-mini-node ${i === 0 ? "c" : /agent|Triage|^Plan|Apply|score/i.test(s) ? "v" : /Approv/i.test(s) ? "g" : ""}`}>
                      <i>{String(i + 1).padStart(2, "0")}</i>{s}
                    </span>
                    {i < wf.flow.length - 1 && <ChevronRight size={14} className="arr" />}
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

/* ---------------------------------------------------------------- metrics */

const tipStyle = {
  background: "var(--cv-s1)", border: "1px solid var(--cv-line2)", borderRadius: 10,
  color: "var(--cv-fg)", fontSize: 12, boxShadow: "0 10px 30px rgba(0,0,0,.35)",
};
const axis = { fill: "var(--cv-faint)", fontSize: 11 };

function RunFeed({ on }: { on: boolean }) {
  const reduced = useReducedMotion();
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!on || reduced) return;
    const id = window.setInterval(() => setTick((t) => t + 1), 2600);
    return () => window.clearInterval(id);
  }, [on, reduced]);
  const rows = Array.from({ length: 4 }, (_, i) => {
    const n = tick + 3 - i; // newest first
    return { n, ...RUN_FEED[n % RUN_FEED.length] };
  });
  return (
    <ul className="cv-feed">
      <AnimatePresence initial={false}>
        {rows.map((r) => (
          <motion.li key={r.n} layout className={r.status}
            initial={{ opacity: 0, y: -14 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }}
            transition={{ duration: 0.3 }}>
            <i /><span className="wf">{r.wf}</span>
            <span className="st">{r.note}</span>
          </motion.li>
        ))}
      </AnimatePresence>
    </ul>
  );
}

export function Metrics() {
  const ref = useRef<HTMLDivElement>(null);
  const seen = useInView(ref, { once: true, margin: "-120px" });
  const live = useInView(ref, { margin: "-80px" });
  return (
    <section className="cv-sec" id="metrics">
      <SectionHead
        kicker="Board & metrics"
        title="See where work is, and where it gets stuck."
        sub="Every ticket is a card that moves Backlog → Implement → Verify → Done. The Metrics view reports the numbers below from your own runs, including what each run cost in model tokens."
      />
      <Reveal>
        <div className="cv-dash" ref={ref}>
          <div className="cv-dash-bar">
            <span className="cv-dots" aria-hidden><i /><i /><i /></span>
            <b>Metrics</b><span className="range">Project · sample</span>
            <DemoTag>Sample values</DemoTag>
          </div>
          <div className="cv-kpis">
            {KPIS.map((k) => (
              <div key={k.label} className={`cv-kpi t-${k.tone}`}>
                <span>{k.label}</span>
                <b><CountUp value={k.value} decimals={k.decimals} prefix={"prefix" in k ? k.prefix : ""} suffix={k.suffix} /></b>
              </div>
            ))}
          </div>
          <div className="cv-dash-grid">
            <div className="cv-dash-card big">
              <h4>Tasks by stage <small>where work is right now</small></h4>
              <div className="cv-chart">
                {seen && (
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={BY_STAGE} margin={{ top: 8, right: 8, left: -22, bottom: 0 }}>
                      <CartesianGrid stroke="var(--cv-line)" vertical={false} />
                      <XAxis dataKey="stage" tick={axis} axisLine={false} tickLine={false} />
                      <YAxis tick={axis} axisLine={false} tickLine={false} allowDecimals={false} />
                      <Tooltip contentStyle={tipStyle} cursor={{ fill: "var(--cv-line)" }} />
                      <Bar dataKey="n" name="Tasks" fill="#8B5CF6" radius={[5, 5, 0, 0]} animationDuration={1000} />
                    </BarChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>
            <div className="cv-dash-card">
              <h4>Average time in stage <small>minutes</small></h4>
              <div className="cv-chart">
                {seen && (
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={STAGE_DWELL} margin={{ top: 8, right: 4, left: -22, bottom: 0 }}>
                      <CartesianGrid stroke="var(--cv-line)" vertical={false} />
                      <XAxis dataKey="stage" tick={axis} axisLine={false} tickLine={false} />
                      <YAxis tick={axis} axisLine={false} tickLine={false} />
                      <Tooltip contentStyle={tipStyle} cursor={{ fill: "var(--cv-line)" }} />
                      <Bar dataKey="min" name="Minutes" fill="#22D3EE" radius={[5, 5, 0, 0]} animationDuration={1000} />
                    </BarChart>
                  </ResponsiveContainer>
                )}
              </div>
            </div>
            <div className="cv-dash-card wide">
              <h4>Recent runs <small>most recent first</small></h4>
              <RunFeed on={live} />
            </div>
          </div>
        </div>
      </Reveal>
    </section>
  );
}

/* ------------------------------------------------------------ open source */

const OSS_POINTS = [
  "MIT licensed. Read it, fork it, change it.",
  "One Go binary with the web UI embedded. No separate frontend to deploy.",
  "PostgreSQL for durable state, or in-memory to try it out.",
  "Your repository token and model key stay in your own deployment, encrypted.",
];

export function OpenSource({ onGetStarted }: { onGetStarted: () => void }) {
  return (
    <section className="cv-sec" id="opensource">
      <SectionHead kicker="Open source" title="Self-hosted. Yours to run." />
      <Reveal>
        <div className="cv-oss">
          <div className="cv-oss-copy">
            <ul>
              {OSS_POINTS.map((p) => <li key={p}><Check size={15} />{p}</li>)}
            </ul>
            <div className="cv-cta-row">
              <button className="cv-btn primary" onClick={onGetStarted}>Get started <ArrowRight size={16} /></button>
              <a className="cv-btn ghost" href={REPO_URL} target="_blank" rel="noreferrer"><Github size={15} /> View on GitHub</a>
              <a className="cv-btn ghost" href={DOCS_URL}><BookOpen size={15} /> Technical design</a>
            </div>
          </div>
          <div className="cv-term" aria-label="Quickstart commands">
            <div className="cv-term-bar"><span className="cv-dots" aria-hidden><i /><i /><i /></span><b>Quickstart</b></div>
            <pre><code>
              <span className="c"># run with Docker (app + Postgres)</span>{"\n"}
              <span className="p">$</span> git clone {REPO_URL}{"\n"}
              <span className="p">$</span> cd conduit{"\n"}
              <span className="p">$</span> make up{"\n\n"}
              <span className="c"># or build the single binary from source</span>{"\n"}
              <span className="p">$</span> make build{"\n"}
              <span className="p">$</span> ./conduitd{"\n\n"}
              <span className="c"># then open http://localhost:8080</span>
            </code></pre>
          </div>
        </div>
      </Reveal>
    </section>
  );
}

/* -------------------------------------------------------------------- faq */

export function Faq() {
  const [open, setOpen] = useState<number | null>(0);
  return (
    <section className="cv-sec narrow" id="faq">
      <SectionHead kicker="FAQ" title="Questions, answered plainly." />
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
        <h2>Keep the judgment.<br /><span className="cv-grad">Hand off the keystrokes.</span></h2>
        <p>Point Conduit at a repository, give it a ticket, and review the pull request it opens.</p>
        <div className="cv-cta-row ctr">
          <button className="cv-btn primary lg" onClick={onGetStarted}>Get started <ArrowRight size={17} /></button>
          <a className="cv-btn ghost lg" href={REPO_URL} target="_blank" rel="noreferrer"><Github size={16} /> View on GitHub</a>
        </div>
      </Reveal>
    </section>
  );
}
