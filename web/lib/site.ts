// Canonical public origin, used for metadataBase, canonical URLs, OG/Twitter
// image URLs, robots.txt and sitemap.xml. Set NEXT_PUBLIC_SITE_URL to your real
// deployed domain (e.g. https://conduit.yourco.com) so Google indexes the right
// host and social cards resolve to absolute URLs. The fallback is only a
// placeholder for local builds.
export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "https://conduit.app").replace(/\/+$/, "");

export const SITE_NAME = "Conduit";
export const SITE_TAGLINE = "Tickets in. Reviewed pull requests out.";
export const SITE_DESCRIPTION =
  "Conduit is an open-source, self-hosted AI software factory: a Jira, Notion or GitHub ticket becomes a CI-verified pull request through triage, planning, human approval and a coding agent, on a visual workflow graph you can watch live.";
