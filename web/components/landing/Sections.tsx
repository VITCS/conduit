"use client";
import { useEffect, useState } from "react";
import { motion, useReducedMotion } from "framer-motion";
import {
  ArrowRight, BarChart3, Bot, CalendarClock, Check, Handshake, Headphones, KeyRound, Lock,
  Megaphone, MousePointer2, Plug, RefreshCw, Search, Settings2, Sparkles, UserCheck, Workflow, X,
} from "lucide-react";
import { AGENTS, CATALOG_URL, INTEGRATIONS, SUPPORT_LABEL, SUPPORT_SHORT, type Support } from "./data";
import { EASE, Reveal, SectionHead } from "./ui";

const stagger = { hidden: {}, show: { transition: { staggerChildren: 0.16, delayChildren: 0.15 } } };
const item = { hidden: { opacity: 0, y: 8 }, show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: EASE } } };
const inView = { initial: "hidden", whileInView: "show", viewport: { once: true, margin: "-60px" } } as const;

/* ------------------------------------------------------------ how it works */

export function HowItWorks() {
  return (
    <section className="cv-sec" id="how">
      <SectionHead kicker="How it works" title="From idea to automation in minutes." />
      <div className="cv-steps">
        <span className="cv-steps-line" aria-hidden />
        <Reveal className="cv-step">
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-art-connect">
              {["Slack", "GitHub", "Notion", "REST API"].map((t) => (
                <motion.span key={t} variants={item} className="cv-chip"><Check size={11} /> {t}</motion.span>
              ))}
              <motion.span variants={item} className="cv-art-plug"><Plug size={18} /></motion.span>
            </div>
          </motion.div>
          <span className="cv-step-n">01</span>
          <h3>Connect Your Tools</h3>
          <p>Connect your existing applications using simple integrations.</p>
        </Reveal>

        <Reveal className="cv-step" delay={0.1}>
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-art-build">
              <motion.span variants={item} className="cv-mini-node c">Trigger</motion.span>
              <motion.i variants={item} />
              <motion.span variants={item} className="cv-mini-node v"><Bot size={12} /> AI Agent</motion.span>
              <motion.i variants={item} />
              <motion.span className="cv-mini-node g drop"
                variants={{ hidden: { opacity: 0, x: 34, y: -22 }, show: { opacity: 1, x: 0, y: 0, transition: { duration: 0.6, ease: EASE } } }}>
                IF / ELSE <MousePointer2 size={12} className="cur" />
              </motion.span>
            </div>
          </motion.div>
          <span className="cv-step-n">02</span>
          <h3>Design Your Workflow</h3>
          <p>Build workflows visually using drag-and-drop blocks, AI agents, and conditional logic.</p>
        </Reveal>

        <Reveal className="cv-step" delay={0.2}>
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-art-run">
              {["Lead scored · 87", "Sales notified", "Email drafted"].map((t) => (
                <motion.div key={t} variants={item} className="cv-run-row"><span><Check size={11} /></span>{t}</motion.div>
              ))}
              <motion.div variants={item} className="cv-run-row live"><span><i /></span>Waiting for next event</motion.div>
            </div>
          </motion.div>
          <span className="cv-step-n">03</span>
          <h3>Let AI Execute</h3>
          <p>Your workflows run automatically, handle decisions, and keep your business moving.</p>
        </Reveal>
      </div>
    </section>
  );
}

/* ----------------------------------------------------------------- agents */

const AGENT_ICONS = {
  search: Search, handshake: Handshake, headphones: Headphones,
  chart: BarChart3, megaphone: Megaphone, settings: Settings2,
} as const;

function AgentCard({ agent, index }: { agent: (typeof AGENTS)[number]; index: number }) {
  const [active, setActive] = useState(false);
  const [step, setStep] = useState(0);
  const reduced = useReducedMotion();
  const Icon = AGENT_ICONS[agent.icon as keyof typeof AGENT_ICONS];

  // While the card is open, play the agent's three-step run on a loop.
  useEffect(() => {
    if (!active) { setStep(0); return; }
    if (reduced) { setStep(3); return; }
    const id = window.setInterval(() => setStep((s) => (s >= 3 ? 0 : s + 1)), 800);
    return () => window.clearInterval(id);
  }, [active, reduced]);

  return (
    <Reveal delay={(index % 3) * 0.08}>
      <article
        className={`cv-agent${active ? " on" : ""}`}
        onMouseEnter={() => setActive(true)} onMouseLeave={() => setActive(false)}
        onFocus={() => setActive(true)} onBlur={() => setActive(false)}
        onClick={() => setActive((a) => !a)}
        tabIndex={0} role="button" aria-expanded={active}
        aria-label={`${agent.name}: ${agent.desc}`}
      >
        <header>
          <span className="cv-agent-ico"><Icon size={18} /></span>
          <span className={`cv-agent-live${active ? " run" : ""}`}><i />{active ? "running" : "ready"}</span>
        </header>
        <h3>{agent.name}</h3>
        <p>{agent.desc}</p>
        <div className="cv-agent-tools">
          {agent.tools.map((t) => <span key={t} className="cv-chip sm">{t}</span>)}
        </div>
        <div className="cv-agent-more" aria-hidden={!active}>
          <div className="cv-agent-task"><Sparkles size={12} /> <span>{agent.task}</span></div>
          <ol className="cv-agent-run">
            {agent.steps.map((s, i) => (
              <li key={s} className={step > i ? "done" : step === i && active ? "cur" : ""}>
                <span>{step > i ? <Check size={10} /> : i + 1}</span>{s}
              </li>
            ))}
          </ol>
        </div>
      </article>
    </Reveal>
  );
}

export function Agents() {
  return (
    <section className="cv-sec" id="agents">
      <SectionHead
        kicker="AI agents"
        title="More than automation. Intelligent execution."
        sub="Agents understand context, make decisions, use your tools and carry out multi-step tasks — inside a workflow you can see and gate."
      />
      <div className="cv-agents">
        {AGENTS.map((a, i) => <AgentCard key={a.id} agent={a} index={i} />)}
      </div>
      <p className="cv-note">Example agent roles. Each is an agent step configured with your own prompt, tools and model. Hover or tap a card to watch it run.</p>
    </section>
  );
}

/* ----------------------------------------------------------- integrations */

function orbitPos(i: number, n: number, radius: number, offsetDeg = 0) {
  const a = ((360 / n) * i + offsetDeg) * (Math.PI / 180);
  return { left: `${50 + radius * Math.cos(a)}%`, top: `${50 + radius * Math.sin(a)}%` };
}

function IntegrationTile({ it }: { it: (typeof INTEGRATIONS)[number] }) {
  return (
    <span className={`cv-int sup-${it.support}`} title={`${it.name} — ${SUPPORT_LABEL[it.support]}`}>
      <span className="cv-int-mark" style={{ color: it.color, borderColor: `${it.color}55`, background: `${it.color}14` }}>{it.mark}</span>
      <span className="cv-int-name">{it.name}</span>
    </span>
  );
}

export function Integrations() {
  const ring1 = INTEGRATIONS.filter((i) => i.ring === 1);
  const ring2 = INTEGRATIONS.filter((i) => i.ring === 2);
  const legend: Support[] = ["native", "api", "planned"];
  return (
    <section className="cv-sec" id="integrations">
      <SectionHead
        kicker="Integrations"
        title="Everything connected. One intelligent workspace."
        sub="Built-in components for the tools teams live in, and open HTTP, GraphQL and webhook blocks for everything else."
      />
      <Reveal>
        <div className="cv-orbit-wrap">
          <div className="cv-orbit-stage" aria-hidden>
            <span className="cv-orbit-track t1" /><span className="cv-orbit-track t2" />
            <div className="cv-orbit-hub"><Workflow size={22} /><b>Conduit</b><small>AI automation hub</small></div>
            <div className="cv-orbit-ring r1">
              {ring1.map((it, i) => (
                <div key={it.name} className="cv-orbit-item" style={orbitPos(i, ring1.length, 27, -90)}>
                  <div className="cv-orbit-up r1"><IntegrationTile it={it} /></div>
                </div>
              ))}
            </div>
            <div className="cv-orbit-ring r2">
              {ring2.map((it, i) => (
                <div key={it.name} className="cv-orbit-item" style={orbitPos(i, ring2.length, 45, -70)}>
                  <div className="cv-orbit-up r2"><IntegrationTile it={it} /></div>
                </div>
              ))}
            </div>
          </div>
          {/* Readable list: the only version on small screens, and the accessible one everywhere. */}
          <ul className="cv-int-grid">
            {INTEGRATIONS.map((it) => (
              <li key={it.name}><IntegrationTile it={it} /><em className={`sup-${it.support}`}>{SUPPORT_SHORT[it.support]}</em></li>
            ))}
          </ul>
        </div>
        <div className="cv-int-foot">
          <div className="cv-legend">
            {legend.map((s) => <span key={s} className={`sup-${s}`}><i />{SUPPORT_LABEL[s]}</span>)}
          </div>
          <a className="cv-btn ghost" href={CATALOG_URL}>Explore Integrations <ArrowRight size={15} /></a>
        </div>
      </Reveal>
    </section>
  );
}

/* ----------------------------------------------------------- capabilities */

function Cap({ title, body, wide, children, i }: {
  title: string; body: string; wide?: boolean; children: React.ReactNode; i: number;
}) {
  return (
    <Reveal className={`cv-cap${wide ? " wide" : ""}`} delay={(i % 4) * 0.06}>
      <motion.div className="cv-cap-art" variants={stagger} {...inView}>{children}</motion.div>
      <h3>{title}</h3>
      <p>{body}</p>
    </Reveal>
  );
}

function ApprovalMini() {
  const [state, setState] = useState<"pending" | "approved" | "rejected">("pending");
  return (
    <div className="cv-m-approve">
      <div className="q"><UserCheck size={14} /> Send 42 personalised emails?</div>
      {state === "pending" ? (
        <div className="b">
          <button className="ok" onClick={() => setState("approved")}>Approve</button>
          <button onClick={() => setState("rejected")}>Reject</button>
        </div>
      ) : (
        <motion.div className={`r ${state}`} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }}>
          {state === "approved" ? <><Check size={13} /> Approved — run continues</> : <><X size={13} /> Rejected — run stops here</>}
          <button onClick={() => setState("pending")}>Undo</button>
        </motion.div>
      )}
    </div>
  );
}

export function Capabilities() {
  const bars = [38, 52, 44, 68, 58, 82, 74];
  return (
    <section className="cv-sec" id="capabilities">
      <SectionHead
        kicker="Capabilities"
        title="Everything a workflow needs to run on its own."
        sub="Twelve building blocks, one canvas. Each one is a real part of the product."
      />
      <div className="cv-caps">
        <Cap i={0} wide title="Visual Workflow Builder" body="Drag blocks onto a canvas, connect them, and see the whole automation at a glance.">
          <div className="cv-m-builder">
            <motion.span variants={item} className="cv-mini-node c">Webhook</motion.span>
            <motion.i variants={item} />
            <motion.span variants={item} className="cv-mini-node v"><Bot size={12} /> Agent</motion.span>
            <motion.i variants={item} />
            <motion.span variants={item} className="cv-mini-node">Slack</motion.span>
            <motion.span className="cv-mini-node g ghost"
              variants={{ hidden: { opacity: 0, x: 30, y: 26 }, show: { opacity: 1, x: 0, y: 0, transition: { duration: 0.6, ease: EASE } } }}>
              + Approval <MousePointer2 size={12} className="cur" />
            </motion.span>
          </div>
        </Cap>

        <Cap i={1} title="Autonomous AI Agents" body="Agents read context, call tools and hand results to the next step.">
          <div className="cv-m-lines">
            {["read ticket #482", "tool · search_docs", "draft reply"].map((t) => (
              <motion.div key={t} variants={item}><em>›</em> {t}</motion.div>
            ))}
          </div>
        </Cap>

        <Cap i={2} title="Intelligent Decision Making" body="Scores and classifications become signals your workflow can route on.">
          <div className="cv-m-gauge">
            <div className="track"><motion.i variants={{ hidden: { width: "0%" }, show: { width: "87%", transition: { duration: 0.9, ease: EASE } } }} /></div>
            <motion.div variants={item} className="lbl"><b>87</b> lead score · <span>high priority</span></motion.div>
          </div>
        </Cap>

        <Cap i={3} title="Conditional Branching" body="IF / ELSE and filter blocks send each run down the right path.">
          <svg className="cv-m-branch" viewBox="0 0 200 70" aria-hidden>
            <motion.path d="M6 35 H70" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.4 } } }} />
            <motion.path className="a" d="M70 35 C100 35 100 12 130 12 H194" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.6 } } }} />
            <motion.path className="b" d="M70 35 C100 35 100 58 130 58 H194" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.6 } } }} />
            <circle cx="70" cy="35" r="5" />
            <text x="136" y="8">yes</text><text x="136" y="68">no</text>
          </svg>
        </Cap>

        <Cap i={4} title="Multi-step Automation" body="Chain as many steps as the job needs; each passes its result on.">
          <div className="cv-m-stepper">
            {[1, 2, 3, 4].map((n) => (
              <motion.span key={n} variants={{ hidden: { opacity: 0.25, scale: 0.9 }, show: { opacity: 1, scale: 1, transition: { duration: 0.3 } } }}>
                <Check size={11} />
              </motion.span>
            ))}
          </div>
        </Cap>

        <Cap i={5} wide title="Real-time Execution Monitoring" body="Every run streams live: status, output and logs for each step as it happens.">
          <div className="cv-m-monitor">
            {[["Lead routing", "ok", "2.1s"], ["Invoice match", "wait", "approval"], ["Daily brief", "run", "step 3/5"]].map(([n, s, m]) => (
              <motion.div key={n} variants={item} className={`row ${s}`}><i /><span>{n}</span><em>{m}</em></motion.div>
            ))}
          </div>
        </Cap>

        <Cap i={6} title="API & Webhook Integrations" body="Start a run from any system and call any HTTP or GraphQL API.">
          <div className="cv-m-api">
            <motion.code variants={item}><b>POST</b> /api/hooks/wf_leads</motion.code>
            <motion.span variants={item} className="ok">201 · run queued</motion.span>
          </div>
        </Cap>

        <Cap i={7} title="Error Handling & Automatic Retries" body="Failed steps retry with backoff, then route to a fallback path.">
          <div className="cv-m-retry">
            <motion.span variants={item} className="bad"><X size={11} /> attempt 1</motion.span>
            <motion.span variants={item} className="mid"><RefreshCw size={11} /> retry</motion.span>
            <motion.span variants={item} className="ok"><Check size={11} /> attempt 2</motion.span>
          </div>
        </Cap>

        <Cap i={8} wide title="Human Approval Gates" body="Pause a run for a person to approve — with the agent's proposal in front of them. Try it.">
          <ApprovalMini />
        </Cap>

        <Cap i={9} title="Workflow Scheduling" body="Run on a cron schedule or inside a business-hours window.">
          <div className="cv-m-cron">
            <motion.code variants={item}>0 9 * * 1-5</motion.code>
            <motion.span variants={item}><CalendarClock size={12} /> next: Mon 09:00</motion.span>
          </div>
        </Cap>

        <Cap i={10} title="Execution History & Analytics" body="Run history, stage timings and throughput for every workflow.">
          <div className="cv-m-bars">
            {bars.map((h, k) => (
              <motion.i key={k} variants={{ hidden: { height: "8%" }, show: { height: `${h}%`, transition: { duration: 0.5, ease: EASE } } }} />
            ))}
          </div>
        </Cap>

        <Cap i={11} wide title="Secure Credential Management" body="Keys live as encrypted secrets and are referenced by name — never pasted into a step.">
          <div className="cv-m-secret">
            <motion.div variants={item}><KeyRound size={13} /><span>crm_api_key</span><em>••••••••••••</em><Lock size={12} /></motion.div>
            <motion.div variants={item}><KeyRound size={13} /><span>llm_key</span><em>••••••••••••</em><Lock size={12} /></motion.div>
            <motion.code variants={item}>{"token: ${secret:crm_api_key}"}</motion.code>
          </div>
        </Cap>
      </div>
    </section>
  );
}
