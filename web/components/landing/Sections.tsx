"use client";
import { useEffect, useState } from "react";
import { motion, useReducedMotion } from "framer-motion";
import {
  ArrowRight, Bot, CalendarClock, Check, ClipboardCheck, Code2, Gauge, GitPullRequest, KeyRound, ListChecks,
  Lock, MousePointer2, PenLine, RefreshCw, SkipForward, Sparkles, UserCheck, Workflow, X,
} from "lucide-react";
import { AGENTS, CATALOG_URL, INTEGRATIONS } from "./data";
import { EASE, Reveal, SectionHead } from "./ui";

const stagger = { hidden: {}, show: { transition: { staggerChildren: 0.16, delayChildren: 0.15 } } };
const item = { hidden: { opacity: 0, y: 8 }, show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: EASE } } };
const inView = { initial: "hidden", whileInView: "show", viewport: { once: true, margin: "-60px" } } as const;

/* ------------------------------------------------------------ how it works */

export function HowItWorks() {
  return (
    <section className="cv-sec" id="how">
      <SectionHead kicker="Getting started" title="Three things to set up. Then it runs." />
      <div className="cv-steps">
        <span className="cv-steps-line" aria-hidden />
        <Reveal className="cv-step">
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-m-secret">
              <motion.div variants={item}><KeyRound size={13} /><span>github_token</span><em>••••••••</em><Lock size={12} /></motion.div>
              <motion.div variants={item}><KeyRound size={13} /><span>llm_key</span><em>••••••••</em><Lock size={12} /></motion.div>
              <motion.code variants={item}>repo: your-org/your-repo</motion.code>
            </div>
          </motion.div>
          <span className="cv-step-n">01</span>
          <h3>Connect a repo and a model</h3>
          <p>Add your GitHub repository and token, and a model API key, as project settings. Secrets are stored encrypted.</p>
        </Reveal>

        <Reveal className="cv-step" delay={0.1}>
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-art-connect">
              {["Board task", "Jira", "Notion", "GitHub issue"].map((t) => (
                <motion.span key={t} variants={item} className="cv-chip"><Check size={11} /> {t}</motion.span>
              ))}
            </div>
          </motion.div>
          <span className="cv-step-n">02</span>
          <h3>Give it a ticket</h3>
          <p>Add a task on the board, or let tickets arrive from Jira, Notion or GitHub issues. Each one gets a confidence score.</p>
        </Reveal>

        <Reveal className="cv-step" delay={0.2}>
          <motion.div className="cv-step-art" variants={stagger} {...inView}>
            <div className="cv-art-run">
              {["Plan approved", "Files approved", "Diff approved", "CI checks passed"].map((t) => (
                <motion.div key={t} variants={item} className="cv-run-row"><span><Check size={11} /></span>{t}</motion.div>
              ))}
              <motion.div variants={item} className="cv-run-row live"><span><i /></span>Pull request opened</motion.div>
            </div>
          </motion.div>
          <span className="cv-step-n">03</span>
          <h3>Approve, then review the PR</h3>
          <p>You approve the plan, the exact files and the final diff. Agents write the change, CI verifies it, and a pull request opens for review.</p>
        </Reveal>
      </div>
    </section>
  );
}

/* ----------------------------------------------------------------- agents */

const AGENT_ICONS = {
  gauge: Gauge, list: ListChecks, pen: PenLine, check: ClipboardCheck, code: Code2, git: GitPullRequest,
} as const;

function AgentCard({ agent, index }: { agent: (typeof AGENTS)[number]; index: number }) {
  const [active, setActive] = useState(false);
  const [step, setStep] = useState(0);
  const reduced = useReducedMotion();
  const Icon = AGENT_ICONS[agent.icon as keyof typeof AGENT_ICONS];

  // While the card is open, play the agent's three steps on a loop.
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
          <span className="cv-agent-live"><code>{agent.step}</code></span>
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
        kicker="The agents"
        title="Six agent steps. Each does one job."
        sub="No black box: every agent is a node on the graph with its own prompt and model, and a person decides at the gates between them."
      />
      <div className="cv-agents">
        {AGENTS.map((a, i) => <AgentCard key={a.id} agent={a} index={i} />)}
      </div>
      <p className="cv-note">These are the agent steps in the two workflows that ship with Conduit. The tag on each card is its component type. Hover or tap a card to see what it does.</p>
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
    <span className="cv-int" title={`${it.name} — ${it.role}`}>
      <span className="cv-int-mark" style={{ color: it.color, borderColor: `${it.color}55`, background: `${it.color}14` }}>{it.mark}</span>
      <span className="cv-int-name">{it.name}</span>
    </span>
  );
}

export function Integrations() {
  const ring1 = INTEGRATIONS.filter((i) => i.ring === 1);
  const ring2 = INTEGRATIONS.filter((i) => i.ring === 2);
  return (
    <section className="cv-sec" id="integrations">
      <SectionHead
        kicker="Integrations"
        title="Plugs into the tools your team already ships with."
        sub="Tickets come from Jira, Notion and GitHub. Code goes to GitHub. The model is yours to choose. HTTP, GraphQL and webhook blocks reach everything else."
      />
      <Reveal>
        <div className="cv-orbit-wrap">
          <div className="cv-orbit-stage" aria-hidden>
            <span className="cv-orbit-track t1" /><span className="cv-orbit-track t2" />
            <div className="cv-orbit-hub"><Workflow size={22} /><b>Conduit</b><small>workflow engine</small></div>
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
              <li key={it.name}><IntegrationTile it={it} /><em>{it.role}</em></li>
            ))}
          </ul>
        </div>
        <div className="cv-int-foot">
          <p className="cv-int-note">Every item here is a built-in component, trigger or backend. Nothing on this list is a roadmap item.</p>
          <a className="cv-btn ghost" href={CATALOG_URL}>See the component catalog <ArrowRight size={15} /></a>
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
      <div className="q"><UserCheck size={14} /> Push 4 files to branch conduit/ENG-482?</div>
      {state === "pending" ? (
        <div className="b">
          <button className="ok" onClick={() => setState("approved")}>Approve</button>
          <button onClick={() => setState("rejected")}>Reject</button>
        </div>
      ) : (
        <motion.div className={`r ${state}`} initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }}>
          {state === "approved" ? <><Check size={13} /> Approved — the run continues</> : <><X size={13} /> Rejected — nothing is written</>}
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
        kicker="What's in the box"
        title="Built for runs you can trust, stop and repeat."
        sub="Every tile below is a feature that exists in the product today."
      />
      <div className="cv-caps">
        <Cap i={0} wide title="Visual workflow canvas" body="Every workflow is a graph you can read, edit and watch run. Drag blocks on, connect them, save.">
          <div className="cv-m-builder">
            <motion.span variants={item} className="cv-mini-node c">Ticket</motion.span>
            <motion.i variants={item} />
            <motion.span variants={item} className="cv-mini-node v"><Bot size={12} /> Agent</motion.span>
            <motion.i variants={item} />
            <motion.span variants={item} className="cv-mini-node">Open PR</motion.span>
            <motion.span className="cv-mini-node g ghost"
              variants={{ hidden: { opacity: 0, x: 30, y: 26 }, show: { opacity: 1, x: 0, y: 0, transition: { duration: 0.6, ease: EASE } } }}>
              + Approval <MousePointer2 size={12} className="cur" />
            </motion.span>
          </div>
        </Cap>

        <Cap i={1} title="Confidence triage" body="Each ticket is scored before any agent runs. Vague work goes to a person, not to a model.">
          <div className="cv-m-gauge">
            <div className="track"><motion.i variants={{ hidden: { width: "0%" }, show: { width: "86%", transition: { duration: 0.9, ease: EASE } } }} /></div>
            <motion.div variants={item} className="lbl"><b>86</b> confidence · <span>ready to automate</span></motion.div>
          </div>
        </Cap>

        <Cap i={2} title="Code you read before it ships" body="The coding agent writes the files — Claude Code works in a real checkout. You read the diff; only then is the branch pushed.">
          <div className="cv-m-lines">
            {["plan · 4 files", "write app/sitemap.ts", "review diff · +31 −2", "push conduit/ENG-482"].map((t) => (
              <motion.div key={t} variants={item}><em>›</em> {t}</motion.div>
            ))}
          </div>
        </Cap>

        <Cap i={3} title="Outcome-based routing" body="Edges carry an outcome: success, failed, approved. Failures route to a visible status, not a silent stop.">
          <svg className="cv-m-branch" viewBox="0 0 200 70" aria-hidden>
            <motion.path d="M6 35 H70" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.4 } } }} />
            <motion.path className="a" d="M70 35 C100 35 100 12 130 12 H194" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.6 } } }} />
            <motion.path className="b" d="M70 35 C100 35 100 58 130 58 H194" variants={{ hidden: { pathLength: 0 }, show: { pathLength: 1, transition: { duration: 0.6 } } }} />
            <circle cx="70" cy="35" r="5" />
            <text x="136" y="8">success</text><text x="136" y="68">failed</text>
          </svg>
        </Cap>

        <Cap i={4} title="CI verification with retry" body="A failed check loops back to the coding agent with the failure as feedback, at most twice.">
          <div className="cv-m-retry">
            <motion.span variants={item} className="bad"><X size={11} /> checks failed</motion.span>
            <motion.span variants={item} className="mid"><RefreshCw size={11} /> fix</motion.span>
            <motion.span variants={item} className="ok"><Check size={11} /> checks passed</motion.span>
          </div>
        </Cap>

        <Cap i={5} wide title="Live run monitoring" body="Step status streams to the canvas as it happens: yet to start, in progress, done, failed, skipped. No refresh.">
          <div className="cv-m-monitor">
            {[["AI Factory · ENG-482", "run", "Apply changes"], ["GitHub review · #129", "wait", "awaiting approval"], ["AI Factory · ENG-479", "ok", "PR opened"]].map(([n, s, m]) => (
              <motion.div key={n} variants={item} className={`row ${s}`}><i /><span>{n}</span><em>{m}</em></motion.div>
            ))}
          </div>
        </Cap>

        <Cap i={6} title="Skip and re-run any stage" body="Bypass a step with a checkbox on its node. Re-run a failed run from the stage that failed. Cancel any time.">
          <div className="cv-m-retry">
            <motion.span variants={item} className="mid"><SkipForward size={11} /> skipped</motion.span>
            <motion.span variants={item} className="bad"><X size={11} /> failed</motion.span>
            <motion.span variants={item} className="ok"><RefreshCw size={11} /> re-run from here</motion.span>
          </div>
        </Cap>

        <Cap i={7} title="Ticket intake webhook" body="Post a GitHub issue or Jira ticket to the board. It lands in Backlog, scored.">
          <div className="cv-m-api">
            <motion.code variants={item}><b>POST</b> /api/intake/{"{project}"}?source=github</motion.code>
            <motion.span variants={item} className="ok">task created · confidence 86</motion.span>
          </div>
        </Cap>

        <Cap i={8} wide title="Human approval gates" body="A run pauses with the plan, the file list or the full diff in front of the approver — approve, send it back with a note, or reject. Try it.">
          <ApprovalMini />
        </Cap>

        <Cap i={9} title="Schedules and time windows" body="Start a workflow on a cron schedule, or hold a step until a time window opens.">
          <div className="cv-m-cron">
            <motion.code variants={item}>0 9 * * 1-5</motion.code>
            <motion.span variants={item}><CalendarClock size={12} /> weekdays at 09:00</motion.span>
          </div>
        </Cap>

        <Cap i={10} title="Board and delivery metrics" body="Lead time, time per stage, CI retry rate and how often a person had to step in.">
          <div className="cv-m-bars">
            {bars.map((h, k) => (
              <motion.i key={k} variants={{ hidden: { height: "8%" }, show: { height: `${h}%`, transition: { duration: 0.5, ease: EASE } } }} />
            ))}
          </div>
        </Cap>

        <Cap i={11} wide title="Encrypted secrets, referenced by name" body="Tokens and API keys are stored encrypted per project and referenced in a step. They are never pasted into the graph.">
          <div className="cv-m-secret">
            <motion.div variants={item}><KeyRound size={13} /><span>github_token</span><em>••••••••••••</em><Lock size={12} /></motion.div>
            <motion.div variants={item}><KeyRound size={13} /><span>llm_key</span><em>••••••••••••</em><Lock size={12} /></motion.div>
            <motion.code variants={item}>{"token: ${secret:github_token}"}</motion.code>
          </div>
        </Cap>
      </div>
    </section>
  );
}
