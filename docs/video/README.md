# Conduit Agent intro video

Two cuts of the same 35-second, 30 fps intro. Neither has audio, so both read correctly
when they autoplay muted.

| File | Size | Use it for |
|---|---|---|
| `conduit-intro.mp4` | 1920×1080 (16:9) | LinkedIn, X, YouTube, the README |
| `conduit-intro-vertical.mp4` | 1080×1920 (9:16) | Instagram Reels, YouTube Shorts, TikTok |
| `preview.gif` | 960×540, 9 s loop | The animated preview at the top of the project README |

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

`intro.html` (landscape) and `intro-vertical.html` (portrait) hold every scene. They share
the same scenes, text and timing, but the layout differs, so a wording change goes in both.
Each frame is a pure function of time (`render(t)`). Open either file in a browser to
preview it live, then re-render:

```bash
node docs/video/render.js                         # writes docs/video/conduit-intro.mp4
```

```bash
node docs/video/render.js docs/video/conduit-intro-vertical.mp4 --src intro-vertical.html
```

```bash
node docs/video/render.js out.mp4 --stills 5,12   # just PNG stills at those seconds
```

Needs Playwright (Chromium), ffmpeg, the Inter font, and `web/node_modules` installed
(for JetBrains Mono).

After re-rendering the landscape video, regenerate the README preview (the sample-run scene):

```bash
cd docs/video && ffmpeg -y -ss 8.7 -t 9.3 -i conduit-intro.mp4 -vf "fps=12,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" -loop 0 preview.gif
```
