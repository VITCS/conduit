import type { Metadata } from "next";
// Self-hosted, licensed monospace (JetBrains Mono, SIL OFL 1.1). Bundled by Next
// and served from our own origin — no third-party font request for code/mono.
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";
import "./globals.css";
import "@xyflow/react/dist/style.css";

export const metadata: Metadata = {
  title: "Conduit Agent",
  description: "Visual control plane for event-driven automation workflows with a human in the loop",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link
          rel="stylesheet"
          href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap"
        />
        {/* VoltAgent is a dark-first brand: default to dark unless the user picked
            light. Applied before paint to avoid a flash. */}
        <script
          dangerouslySetInnerHTML={{
            __html:
              "try{var t=localStorage.getItem('conduit_theme');document.documentElement.setAttribute('data-theme',t==='light'?'light':'dark');}catch(e){document.documentElement.setAttribute('data-theme','dark');}",
          }}
        />
      </head>
      <body>{children}</body>
    </html>
  );
}
