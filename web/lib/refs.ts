// Resolves ${var:…} / ${secret:…} / ${input.…} / ${memory.…} references in a
// node's config value into a human-readable display + its source, so a node shows
// what it actually reads (owner · acme, token · ••••, number · from event) instead
// of raw template tokens. Pure display — the server does the real resolution.

export type RefSource = "config" | "secret" | "event" | "run" | "literal" | "mixed" | "unset";

export interface Resolved {
  text: string;
  source: RefSource;
}

// ${var:NAME} / ${secret:NAME} use a colon; ${input.PATH} / ${memory.PATH} use a dot.
const REF = /\$\{(?:(var|secret):([A-Za-z0-9_.\-]+)|(input|memory)\.([A-Za-z0-9_.\-]+))\}/g;

export function resolveRef(
  value: string,
  vars: Record<string, string>,
  secretNames: string[],
): Resolved {
  if (!value || !value.includes("${")) {
    return { text: value, source: "literal" };
  }
  const sources = new Set<RefSource>();
  let anyUnset = false;

  const text = value.replace(REF, (_m, colonKind: string, colonName: string, dotKind: string, dotName: string) => {
    const kind = colonKind || dotKind;
    const name = colonName || dotName;
    switch (kind) {
      case "var": {
        if (Object.prototype.hasOwnProperty.call(vars, name)) {
          sources.add("config");
          return vars[name] || "(empty)";
        }
        sources.add("unset");
        anyUnset = true;
        return `⟨set ${name} in Config⟩`;
      }
      case "secret": {
        if (secretNames.includes(name)) {
          sources.add("secret");
          return "••••";
        }
        sources.add("unset");
        anyUnset = true;
        return `⟨set ${name} in Secrets⟩`;
      }
      case "input":
        sources.add("event");
        return "from event";
      case "memory":
        sources.add("run");
        return "from run";
      default:
        return _m;
    }
  });

  let source: RefSource;
  if (anyUnset) source = "unset";
  else if (sources.size === 0) source = "literal";
  else if (sources.size === 1) source = [...sources][0];
  else source = "mixed";

  return { text, source };
}

// Short label shown on the source chip.
export const SOURCE_LABEL: Record<RefSource, string> = {
  config: "config",
  secret: "secret",
  event: "event",
  run: "run",
  literal: "",
  mixed: "mixed",
  unset: "set me",
};
