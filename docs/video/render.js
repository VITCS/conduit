// Renders intro.html to an MP4, one frame at a time (deterministic, no dropped frames).
//   node docs/video/render.js [out.mp4] [--fps 30] [--stills 2,10,20]
// Requires Playwright (Chromium) and ffmpeg on PATH.
const { chromium } = require("playwright");
const { execFileSync } = require("child_process");
const fs = require("fs"), os = require("os"), path = require("path");

const args = process.argv.slice(2);
const opt = (name, def) => { const i = args.indexOf(name); return i >= 0 ? args[i + 1] : def; };
const out = path.resolve(args.find((a) => a.endsWith(".mp4")) || path.join(__dirname, "conduit-intro.mp4"));
const fps = Number(opt("--fps", 30));
const stills = opt("--stills", "");

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 });
  await page.goto("file://" + path.join(__dirname, "intro.html") + "?capture");
  await page.evaluate(() => document.fonts.ready);
  const duration = await page.evaluate(() => window.DURATION);

  if (stills) {
    for (const s of stills.split(",").map(Number)) {
      await page.evaluate((t) => window.render(t), s);
      await page.screenshot({ path: path.join(path.dirname(out), `still-${s}.png`) });
    }
    await browser.close();
    return;
  }

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "conduit-frames-"));
  const total = Math.round(duration * fps);
  for (let f = 0; f < total; f++) {
    await page.evaluate((t) => window.render(t), f / fps);
    await page.screenshot({ path: path.join(dir, String(f).padStart(5, "0") + ".jpg"), type: "jpeg", quality: 95 });
    if (f % 150 === 0) process.stdout.write(`frame ${f}/${total}\n`);
  }
  await browser.close();
  execFileSync("ffmpeg", ["-y", "-loglevel", "error", "-framerate", String(fps), "-i", path.join(dir, "%05d.jpg"),
    "-c:v", "libx264", "-preset", "slow", "-crf", "20", "-pix_fmt", "yuv420p", "-movflags", "+faststart", out], { stdio: "inherit" });
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("wrote", out);
})();
