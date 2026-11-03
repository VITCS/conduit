// Canonical public origin, used for metadataBase, canonical URLs, OG/Twitter
// image URLs, robots.txt and sitemap.xml. Set NEXT_PUBLIC_SITE_URL to your real
// deployed domain (e.g. https://conduit.yourco.com) so Google indexes the right
// host and social cards resolve to absolute URLs. The fallback is only a
// placeholder for local builds.
export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "https://conduit.app").replace(/\/+$/, "");

export const SITE_NAME = "Conduit";
export const SITE_TAGLINE = "Event-driven automation, with a human in the loop";
export const SITE_DESCRIPTION =
  "Conduit is a visual control plane for event-driven automation workflows — build pipelines on a live canvas, review agent output, and keep a human approval gate before anything merges or ships. Self-hosted.";
