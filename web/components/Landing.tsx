"use client";
import { useEffect, useRef, useState } from "react";
import { MotionConfig, motion, useMotionValueEvent, useScroll, useTransform } from "framer-motion";
import { ArrowRight, GitPullRequest, Github, Moon, Play, Sun, Ticket, UserCheck } from "lucide-react";
import "./landing/landing.css";
import { DOCS_URL, NAV_LINKS, REPO_URL } from "./landing/data";
import { HeroCanvas } from "./landing/HeroCanvas";
import { FactoryDemo } from "./landing/FactoryDemo";
import { Agents, Capabilities, HowItWorks, Integrations } from "./landing/Sections";
import { Faq, FinalCta, Metrics, OpenSource, Workflows } from "./landing/Sections2";
import { EASE, Mark } from "./landing/ui";

type Theme = "dark" | "light";
const THEME_KEY = "conduit_landing_theme";

export function Landing({ onSignIn, onGetStarted }: { onSignIn: () => void; onGetStarted: () => void }) {
  const scroller = useRef<HTMLDivElement>(null);
  const [theme, setTheme] = useState<Theme>("dark");
  const [scrolled, setScrolled] = useState(false);

  // The landing is its own scroll container (the app shell doesn't scroll).
  const { scrollY } = useScroll({ container: scroller });
  const auroraY = useTransform(scrollY, [0, 900], [0, 140]);
  useMotionValueEvent(scrollY, "change", (v) => setScrolled(v > 12));

  // Dark-first; remember an explicit light choice. The document theme follows the
  // landing while it is mounted and is restored afterwards for the app.
  useEffect(() => {
    try { if (localStorage.getItem(THEME_KEY) === "light") setTheme("light"); } catch { /* storage unavailable */ }
  }, []);
  useEffect(() => {
    const root = document.documentElement;
    const prev = root.getAttribute("data-theme");
    root.setAttribute("data-theme", theme);
    return () => { if (prev) root.setAttribute("data-theme", prev); };
  }, [theme]);
  const toggleTheme = () => {
    const next: Theme = theme === "dark" ? "light" : "dark";
    setTheme(next);
    try { localStorage.setItem(THEME_KEY, next); } catch { /* storage unavailable */ }
  };

  return (
    <MotionConfig reducedMotion="user">
      <div className="cvl" data-lt={theme} ref={scroller}>
        <motion.div className="cv-bg" style={{ y: auroraY }} aria-hidden>
          <span className="a1" /><span className="a2" /><span className="grid" />
        </motion.div>

        <header className={`cv-nav${scrolled ? " scrolled" : ""}`}>
          <a className="cv-brand" href="#top" aria-label="Conduit Agent — home">
            <Mark /><span>Conduit <em>Agent</em></span>
          </a>
          <nav className="cv-nav-links" aria-label="Primary">
            {NAV_LINKS.map((l) => <a key={l.href} href={l.href}>{l.label}</a>)}
            <a href={DOCS_URL}>Docs</a>
          </nav>
          <div className="cv-nav-r">
            <button className="cv-iconbtn" onClick={toggleTheme}
              aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}>
              {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
            </button>
            <button className="cv-btn ghost sm" onClick={onSignIn} data-testid="nav-signin">Sign in</button>
            <button className="cv-btn primary sm" onClick={onGetStarted} data-testid="nav-getstarted">Get started</button>
          </div>
        </header>

        <main id="top">
          {/* ── hero ── */}
          <section className="cv-hero">
            <motion.span className="cv-eyebrow"
              initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.5, ease: EASE }}>
              <GitPullRequest size={13} /> Open-source AI software factory
            </motion.span>
            <motion.h1
              initial={{ opacity: 0, y: 16 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, ease: EASE, delay: 0.06 }}>
              Tickets in.<br /><span className="cv-grad">Reviewed pull requests out.</span>
            </motion.h1>
            <motion.p className="cv-lead"
              initial={{ opacity: 0, y: 14 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, ease: EASE, delay: 0.14 }}>
              Conduit turns a ticket into a pull request. AI agents triage it, plan the change and write the
              code. You approve the plan, the exact files and the final diff, CI has to pass, and every step
              is a node on a graph you can watch.
            </motion.p>
            <motion.div className="cv-cta-row ctr"
              initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.6, ease: EASE, delay: 0.22 }}>
              <button className="cv-btn primary lg" onClick={onGetStarted} data-testid="hero-getstarted">
                Get started <ArrowRight size={17} />
              </button>
              <a className="cv-btn ghost lg" href="#demo" data-testid="hero-demo"><Play size={15} /> Watch a ticket run</a>
            </motion.div>
            <motion.p className="cv-trust"
              initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.6, delay: 0.34 }}>
              Self-hosted · MIT licensed · bring your own model and repository
            </motion.p>

            <motion.div className="cv-hero-stage"
              initial={{ opacity: 0, y: 28, scale: 0.985 }} animate={{ opacity: 1, y: 0, scale: 1 }}
              transition={{ duration: 0.8, ease: EASE, delay: 0.3 }}>
              <HeroCanvas />
            </motion.div>

            <ul className="cv-pillars" aria-label="What Conduit does">
              <li><Ticket size={16} /> Jira, Notion and GitHub tickets in.</li>
              <li><UserCheck size={16} /> A human approves plan, files and diff.</li>
              <li><GitPullRequest size={16} /> A CI-verified pull request out.</li>
            </ul>
          </section>

          <FactoryDemo />
          <HowItWorks />
          <Agents />
          <Integrations />
          <Workflows />
          <Capabilities />
          <Metrics />
          <OpenSource onGetStarted={onGetStarted} />
          <Faq />
          <FinalCta onGetStarted={onGetStarted} />
        </main>

        <footer className="cv-foot">
          <div className="cv-foot-top">
            <a className="cv-brand" href="#top"><Mark size={24} /><span>Conduit <em>Agent</em></span></a>
            <nav aria-label="Footer">
              {NAV_LINKS.map((l) => <a key={l.href} href={l.href}>{l.label}</a>)}
              <a href={DOCS_URL}>Docs</a>
              <a href={REPO_URL} target="_blank" rel="noreferrer"><Github size={14} /> GitHub</a>
              <button onClick={onSignIn}>Sign in</button>
            </nav>
          </div>
          <p>
            Self-hosted · single binary · MIT licensed · email or Google / GitHub sign-in when the server has those
            providers configured. The tickets, runs and metric values shown on this page are sample data; the
            workflows, steps and integrations are the ones that ship in the product.
          </p>
        </footer>

        {/* always-reachable primary action on small screens */}
        <div className={`cv-mcta${scrolled ? " show" : ""}`}>
          <button className="cv-btn primary block" onClick={onGetStarted}>Get started <ArrowRight size={16} /></button>
        </div>
      </div>
    </MotionConfig>
  );
}
