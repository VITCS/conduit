"use client";
import { useEffect, useRef, useState } from "react";
import { motion, useInView, useReducedMotion } from "framer-motion";

export const EASE = [0.21, 0.6, 0.25, 1] as const;

/** Fade-and-rise an element the first time it scrolls into view. */
export function Reveal({
  children, delay = 0, className, y = 18,
}: { children: React.ReactNode; delay?: number; className?: string; y?: number }) {
  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: "-70px" }}
      transition={{ duration: 0.55, ease: EASE, delay }}
    >
      {children}
    </motion.div>
  );
}

export function SectionHead({
  kicker, title, sub, center = true,
}: { kicker: string; title: React.ReactNode; sub?: React.ReactNode; center?: boolean }) {
  return (
    <Reveal className={`cv-head${center ? " ctr" : ""}`}>
      <span className="cv-kicker">{kicker}</span>
      <h2>{title}</h2>
      {sub && <p>{sub}</p>}
    </Reveal>
  );
}

/** Small "Demo data" marker for illustrative numbers and sample runs. */
export function DemoTag({ children = "Demo data" }: { children?: React.ReactNode }) {
  return <span className="cv-demo-tag">{children}</span>;
}

/** Count from 0 to `value` once the element is visible. */
export function CountUp({
  value, decimals = 0, prefix = "", suffix = "", duration = 1300,
}: { value: number; decimals?: number; prefix?: string; suffix?: string; duration?: number }) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, margin: "-40px" });
  const reduced = useReducedMotion();
  const [n, setN] = useState(0);
  useEffect(() => {
    if (!inView) return;
    if (reduced) { setN(value); return; }
    let raf = 0;
    const start = performance.now();
    const tick = (t: number) => {
      const p = Math.min(1, (t - start) / duration);
      setN(value * (1 - Math.pow(1 - p, 3)));
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [inView, value, duration, reduced]);
  const text = n.toLocaleString("en-US", { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
  return <span ref={ref}>{prefix}{text}{suffix}</span>;
}

/** The Conduit mark, drawn with the landing's own accents. */
export function Mark({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" aria-hidden>
      <defs>
        <linearGradient id="cvMarkG" x1="4" y1="4" x2="28" y2="28" gradientUnits="userSpaceOnUse">
          <stop stopColor="#8B5CF6" />
          <stop offset="1" stopColor="#22D3EE" />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="30" height="30" rx="9" fill="url(#cvMarkG)" opacity=".16" />
      <rect x="1" y="1" width="30" height="30" rx="9" stroke="url(#cvMarkG)" strokeWidth="1.4" />
      <path d="M8 16h5.5c3.6 0 3.6-6.5 7.2-6.5M13.5 16c3.6 0 3.6 6.5 7.2 6.5" stroke="url(#cvMarkG)" strokeWidth="2.3" strokeLinecap="round" />
      <circle cx="22.6" cy="9.5" r="2.3" fill="#22D3EE" />
      <circle cx="22.6" cy="22.5" r="2.3" fill="#8B5CF6" />
    </svg>
  );
}
