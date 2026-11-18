# Conduit Agent intro video

`conduit-intro.mp4` is a 35-second, 1920×1080, 30 fps intro for social posts and the README.
It has no audio, so it reads correctly when it autoplays muted.

| Time | Scene |
|---|---|
| 0–4 s | Hook: a backlog of tickets, and the question "what if each became a reviewed PR?" |
| 4–9 s | Brand: "Tickets in. Reviewed pull requests out." |
| 9–18 s | A sample AI Factory run around the hub: ticket → triage → two approvals → branch → CI → PR |
| 18–24 s | The approval gate: approving the exact file changes, then CI checks and the PR |
| 24–29 s | Getting started in three steps |
| 29–32 s | Built-in integrations |
| 32–35 s | Call to action: open source, self-hosted, MIT, `github.com/VITCS/conduit` |

The ticket, run number and confidence score are sample data and are labelled "sample" on screen.

## Editing and re-rendering

`intro.html` holds every scene; each frame is a pure function of time (`render(t)`).
Open it in a browser to preview it live, then re-render:

```bash
node docs/video/render.js                         # writes docs/video/conduit-intro.mp4
node docs/video/render.js out.mp4 --stills 5,12   # just PNG stills at those seconds
```

Needs Playwright (Chromium), ffmpeg, the Inter font, and `web/node_modules` installed
(for JetBrains Mono).
