"use client";
import { useState } from "react";
import { MessageCircleQuestion, Send } from "lucide-react";

/**
 * A step's question with its suggested answers as buttons, plus a box for your
 * own answer. Used in the run's banner and in the "Needs you" list.
 */
export function QuestionCard(props: {
  question: string;
  options?: string[];
  onAnswer: (answer: string) => Promise<void> | void;
  onStop?: () => void;
  compact?: boolean;
  testid?: string;
}) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);

  async function send(answer: string) {
    const a = answer.trim();
    if (!a || busy) return;
    setBusy(true);
    try {
      await props.onAnswer(a);
      setText("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={`qcard${props.compact ? " compact" : ""}`} data-testid={props.testid ?? "question-card"}>
      <div className="qc-q"><MessageCircleQuestion size={props.compact ? 14 : 16} /> <span>{props.question}</span></div>
      {props.options && props.options.length > 0 && (
        <div className="qc-opts">
          {props.options.map((o) => (
            <button key={o} className="btn qc-opt" disabled={busy} onClick={() => send(o)} data-testid="question-option">{o}</button>
          ))}
        </div>
      )}
      <form className="qc-own" onSubmit={(e) => { e.preventDefault(); send(text); }}>
        <input className="inp" value={text} maxLength={4000} placeholder="Or write your own answer…"
          onChange={(e) => setText(e.target.value)} aria-label="Your answer" data-testid="question-input" />
        <button className="btn primary" type="submit" disabled={busy || !text.trim()} data-testid="question-send">
          <Send size={13} /> Answer
        </button>
        {props.onStop && (
          <button className="btn ghost danger-text" type="button" disabled={busy} onClick={props.onStop}
            title="Stop this run instead of answering">Stop run</button>
        )}
      </form>
    </div>
  );
}
