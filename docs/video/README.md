# Conduit Agent intro video

Two cuts of the same 40-second, 30 fps intro. Each opens on its thumbnail (0.6 s, then a
0.5 s cross-fade into the video), so the frame a feed shows before play is the thumbnail. Neither has audio, so both read correctly
when they autoplay muted.

| File | Size | Use it for |
|---|---|---|
| `conduit-intro.mp4` | 1920×1080 (16:9) | LinkedIn, X, YouTube, the README |
| `conduit-intro-vertical.mp4` | 1080×1920 (9:16) | Instagram Reels, YouTube Shorts, TikTok |
| `thumbnail.png` | 1920×1080 | Custom cover for the landscape video (LinkedIn, YouTube) |
| `thumbnail-vertical.png` | 1080×1920 | Custom cover for the vertical video |
| `preview.gif` | 960×540, 9 s loop | The animated preview at the top of the project README |

| Time | Scene |
|---|---|
| 0–4 s | Hook: a backlog of tickets, and the question "what if each became a reviewed PR?" |
| 4–9 s | Brand: "Tickets in. Reviewed pull requests out." |
| 9–18 s | A sample AI Factory run around the hub: ticket → triage → approve the plan → approve the files → review the diff → branch → CI → PR |
| 18–24 s | The diff review: the actual code before it is pushed, Approve & push, then CI checks and the PR |
| 24–29 s | Models and cost: fast / standard / best model tiers, a step-up to the best model on a CI retry, and a sample run's cost per step and total |
| 29–33 s | Getting started in three steps |
| 33–36 s | Built-in integrations |
| 36–40 s | Call to action: open source, self-hosted, MIT, `github.com/VITCS/conduit` |

The ticket, run numbers, confidence score and the per-step token counts are sample data and are
labelled "sample" on screen. The costs in the models scene are those token counts at the Claude
API list prices shown in the same scene.

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

Then add the thumbnail opening (and refresh the thumbnail images) to both cuts.
A video that already opens on the thumbnail is skipped, so this is safe to re-run:

```bash
node docs/video/thumbnail.js              # use --png-only for just the images
```

The thumbnail design is `thumb.html`; it frames a screenshot of the finished sample run.

Needs Playwright (Chromium), ffmpeg, the Inter font, and `web/node_modules` installed
(for JetBrains Mono).

After re-rendering the landscape video and adding the thumbnail, regenerate the README
preview (the sample-run scene; the offset allows for the 0.6 s thumbnail opening):

```bash
cd docs/video && ffmpeg -y -ss 9.3 -t 9.3 -i conduit-intro.mp4 -vf "fps=12,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" -loop 0 preview.gif
```
