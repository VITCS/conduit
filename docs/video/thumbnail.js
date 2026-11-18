// Builds the thumbnails and puts them at the start of each intro video.
//   node docs/video/thumbnail.js            # after render.js
//   node docs/video/thumbnail.js --png-only # just the thumbnail images
// For each cut it:
//   1. screenshots the finished sample run from intro*.html (t = 17.6 s),
//   2. renders thumb.html around it → thumbnail.png / thumbnail-vertical.png,
//   3. opens the video on the thumbnail (0.6 s) and cross-fades into it (0.5 s),
//      so the frame platforms show before play is the thumbnail.
// A video that already starts with the thumbnail is left alone, so this is safe
// to run twice. Requires Playwright (Chromium) and ffmpeg.
const { chromium } = require("playwright");
const { execFileSync } = require("child_process");
const fs = require("fs"), os = require("os"), path = require("path");

const CUTS = [
  { src: "intro.html", layout: "wide", video: "conduit-intro.mp4", png: "thumbnail.png",
    clip: { x: 60, y: 60, width: 1800, height: 860 } },
  { src: "intro-vertical.html", layout: "tall", video: "conduit-intro-vertical.mp4", png: "thumbnail-vertical.png",
    clip: { x: 40, y: 130, width: 1000, height: 1050 } },
];
const TAG = "conduit-thumbnail-v1";
const HOLD = 0.6, FADE = 0.5;

const comment = (file) => {
  try {
    return execFileSync("ffprobe", ["-v", "error", "-show_entries", "format_tags=comment", "-of", "default=nw=1:nk=1", file]).toString().trim();
  } catch { return ""; }
};

(async () => {
  const browser = await chromium.launch();
  for (const c of CUTS) {
    // 1) the finished run, cropped to its panel
    const page = await browser.newPage({ viewport: { width: 1920, height: 1920 }, deviceScaleFactor: 1 });
    await page.goto("file://" + path.join(__dirname, c.src) + "?capture");
    await page.setViewportSize(await page.evaluate(() => window.SIZE || { width: 1920, height: 1080 }));
    await page.evaluate(() => document.fonts.ready);
    await page.evaluate(() => window.render(17.6));
    const run = await page.screenshot({ clip: c.clip });
    await page.close();

    // 2) the thumbnail
    const tp = await browser.newPage({ viewport: { width: 1920, height: 1920 }, deviceScaleFactor: 1 });
    await tp.goto("file://" + path.join(__dirname, "thumb.html") + "?layout=" + c.layout);
    await tp.setViewportSize(await tp.evaluate(() => window.SIZE));
    await tp.evaluate((img) => window.build(img), "data:image/png;base64," + run.toString("base64"));
    await tp.evaluate(() => Promise.all([document.fonts.ready, ...[...document.images].map((i) => i.decode())]));
    const png = path.join(__dirname, c.png);
    await tp.screenshot({ path: png });
    await tp.close();
    console.log("wrote", png);

    // 3) open the video on it
    if (process.argv.includes("--png-only")) continue;
    const video = path.join(__dirname, c.video);
    if (!fs.existsSync(video)) { console.log("skip (no video yet):", c.video); continue; }
    if (comment(video) === TAG) { console.log("already starts with the thumbnail:", c.video); continue; }
    const tmp = path.join(os.tmpdir(), "conduit-thumb-" + c.video);
    execFileSync("ffmpeg", ["-y", "-loglevel", "error",
      "-loop", "1", "-framerate", "30", "-t", String(HOLD + FADE), "-i", png, "-i", video,
      "-filter_complex", `[0:v]format=yuv420p,setsar=1[a];[1:v]fps=30,format=yuv420p,setsar=1[b];[a][b]xfade=transition=fade:duration=${FADE}:offset=${HOLD},format=yuv420p[v]`,
      "-map", "[v]", "-c:v", "libx264", "-preset", "slow", "-crf", "20", "-pix_fmt", "yuv420p",
      "-movflags", "+faststart", "-metadata", "comment=" + TAG, tmp], { stdio: "inherit" });
    fs.copyFileSync(tmp, video);
    fs.rmSync(tmp, { force: true });
    console.log("added thumbnail opening to", c.video);
  }
  await browser.close();
})();
