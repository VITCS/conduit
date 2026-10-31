/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export: `next build` writes a fully static site to web/out, which the
  // Go server embeds. No Node runtime is needed in production.
  output: "export",
  // The exporter cannot optimize images on the fly; serve them as-is.
  images: { unoptimized: true },
  // Emit index.html files for directory-style routes (harmless for a single page).
  trailingSlash: true,
};

export default nextConfig;
