// Canonical public origin, used for metadataBase, canonical URLs, OG/Twitter
// image URLs, robots.txt and sitemap.xml. Set NEXT_PUBLIC_SITE_URL to your real
// deployed domain (e.g. https://conduit.yourco.com) so Google indexes the right
// host and social cards resolve to absolute URLs. The fallback is only a
// placeholder for local builds.
export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "https://conduit.app").replace(/\/+$/, "");

export const SITE_NAME = "Conduit";
export const SITE_TAGLINE = "AI workflow automation, on autopilot";
export const SITE_DESCRIPTION =
  "Conduit Agent is an AI workflow automation platform: connect your tools, deploy intelligent AI agents, and automate multi-step work on a visual canvas — with human approval gates, live monitoring and self-hosted deployment.";
