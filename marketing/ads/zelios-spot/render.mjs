#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdir, rm, mkdtemp } from "node:fs/promises";
import http from "node:http";
import { createReadStream, existsSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = __dirname;
const DURATION = 22;
const FPS = 30;
const WIDTH = 1920;
const HEIGHT = 1080;
const FRAMES = DURATION * FPS;
const CHROME = process.env.CHROME_PATH || "/usr/local/bin/google-chrome";
const DIST = path.join(ROOT, "dist");
const PREVIEW = process.argv.includes("--preview");
const PREVIEW_TIMES = [0.2, 1.2, 2.7, 4.2, 6.8, 8.7, 10.4, 12.4, 14.0, 16.4, 18.0, 19.6];

function contentType(file) {
  if (file.endsWith(".html")) return "text/html; charset=utf-8";
  if (file.endsWith(".css")) return "text/css";
  if (file.endsWith(".js")) return "text/javascript";
  if (file.endsWith(".png")) return "image/png";
  if (file.endsWith(".woff2")) return "font/woff2";
  if (file.endsWith(".ttf")) return "font/ttf";
  if (file.endsWith(".svg")) return "image/svg+xml";
  return "application/octet-stream";
}

function startServer() {
  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      const urlPath = decodeURIComponent((req.url || "/").split("?")[0]);
      const rel = urlPath === "/" ? "/index.html" : urlPath;
      const file = path.normalize(path.join(ROOT, rel));
      if (!file.startsWith(ROOT)) {
        res.writeHead(403).end();
        return;
      }
      if (!existsSync(file)) {
        res.writeHead(404).end("not found");
        return;
      }
      res.writeHead(200, { "Content-Type": contentType(file) });
      createReadStream(file).pipe(res);
    });
    server.listen(0, "127.0.0.1", () => {
      resolve({ server, port: server.address().port });
    });
  });
}

function run(cmd, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { stdio: "inherit" });
    child.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`${cmd} ${code}`))));
  });
}

function freePort() {
  return new Promise((resolve) => {
    const s = http.createServer();
    s.listen(0, "127.0.0.1", () => {
      const p = s.address().port;
      s.close(() => resolve(p));
    });
  });
}

async function waitForDevtools(port, timeoutMs = 20000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/version`);
      if (res.ok) return await res.json();
    } catch {
      /* still booting */
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(`chrome DevTools on :${port} never came up`);
}

async function launchChrome() {
  const profile = await mkdtemp(path.join(os.tmpdir(), "ibb-ad-chrome-"));
  const dbgPort = await freePort();
  const child = spawn(
    CHROME,
    [
      "--headless=new",
      `--user-data-dir=${profile}`,
      `--remote-debugging-port=${dbgPort}`,
      "--remote-debugging-address=127.0.0.1",
      `--window-size=${WIDTH},${HEIGHT}`,
      "--hide-scrollbars",
      "--disable-lcd-text",
      "--font-render-hinting=none",
      "--force-device-scale-factor=1",
      "--default-background-color=0a0a0a",
      "--no-sandbox",
      "--disable-setuid-sandbox",
      "--disable-dev-shm-usage",
      "--disable-gpu",
      "--no-first-run",
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"] }
  );
  child.stderr.on("data", (buf) => {
    if (process.env.DEBUG_CHROME) process.stderr.write(buf);
  });
  await waitForDevtools(dbgPort);
  const browser = await puppeteer.connect({
    browserURL: `http://127.0.0.1:${dbgPort}`,
    defaultViewport: { width: WIDTH, height: HEIGHT, deviceScaleFactor: 1 },
  });
  return { browser, child };
}

async function paint(page, t) {
  await page.evaluate((time) => window.setAdTime(time), t);
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
}

async function main() {
  await mkdir(DIST, { recursive: true });
  const { server, port } = await startServer();
  const url = `http://127.0.0.1:${port}/index.html`;
  console.log("serving", url);

  const { browser, child: chromeProc } = await launchChrome();
  const page = await browser.newPage();
  await page.setViewport({ width: WIDTH, height: HEIGHT, deviceScaleFactor: 1 });
  await page.goto(url, { waitUntil: "load", timeout: 30000 });
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() =>
    Promise.all([...document.images].map((img) => (img.decode ? img.decode() : Promise.resolve()).catch(() => {})))
  );
  await page.evaluate(() => {
    window.__FRAME_MODE__ = true;
  });

  const shutdown = async () => {
    try { await browser.close(); } catch {}
    try { chromeProc.kill("SIGTERM"); } catch {}
    try { server.close(); } catch {}
  };

  if (PREVIEW) {
    const dir = path.join(DIST, "previews");
    await rm(dir, { recursive: true, force: true });
    await mkdir(dir, { recursive: true });
    for (const t of PREVIEW_TIMES) {
      await paint(page, t);
      const name = `t-${String(t).replace(".", "p")}.png`;
      await page.screenshot({ path: path.join(dir, name), type: "png" });
      console.log("preview", name);
    }
    await shutdown();
    return;
  }

  const videoOut = path.join(DIST, "picture.mp4");
  const ffmpeg = spawn(
    "ffmpeg",
    [
      "-y",
      "-f", "image2pipe",
      "-framerate", String(FPS),
      "-vcodec", "mjpeg",
      "-i", "-",
      "-an",
      "-c:v", "libx264",
      "-pix_fmt", "yuv420p",
      "-preset", "fast",
      "-crf", "18",
      "-movflags", "+faststart",
      videoOut,
    ],
    { stdio: ["pipe", "inherit", "inherit"] }
  );

  const t0 = Date.now();
  for (let i = 0; i < FRAMES; i++) {
    const t = i / FPS;
    await paint(page, t);
    const buf = await page.screenshot({ type: "jpeg", quality: 94 });
    ffmpeg.stdin.write(buf);
    if (i % 30 === 0) {
      const elapsed = (Date.now() - t0) / 1000;
      const fps = (i + 1) / Math.max(elapsed, 0.01);
      console.log(`frame ${i + 1}/${FRAMES}  t=${t.toFixed(2)}  ${fps.toFixed(1)} fps`);
    }
  }
  ffmpeg.stdin.end();
  await new Promise((resolve, reject) => {
    ffmpeg.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`ffmpeg ${code}`))));
  });

  await shutdown();

  const vo = path.join(ROOT, "audio/vo.wav");
  const music = path.join(ROOT, "audio/music.wav");
  const mix = path.join(DIST, "mix.wav");
  await run("ffmpeg", [
    "-y",
    "-i", vo,
    "-i", music,
    "-filter_complex",
    `[0:a]aformat=sample_fmts=fltp:channel_layouts=stereo,volume=1.2,apad=pad_dur=2[v];[1:a]aformat=sample_fmts=fltp:channel_layouts=stereo,volume=0.9,atrim=0:${DURATION}[m];[v][m]amix=inputs=2:duration=longest:dropout_transition=0,alimiter=limit=0.94:level=false,atrim=0:${DURATION}[a]`,
    "-map", "[a]",
    "-ar", "48000",
    mix,
  ]);

  const finalOut = path.join(DIST, "itsbagelbot-zelios-16x9.mp4");
  await run("ffmpeg", [
    "-y",
    "-i", videoOut,
    "-i", mix,
    "-c:v", "libx264",
    "-pix_fmt", "yuv420p",
    "-preset", "slow",
    "-crf", "18",
    "-c:a", "aac",
    "-b:a", "192k",
    "-t", String(DURATION),
    "-movflags", "+faststart",
    "-metadata", "title=ItsBagelBot — Your Stream. Your Tools. Your Rules.",
    "-metadata", "artist=ItsBagelBot",
    finalOut,
  ]);

  const webDir = path.join(ROOT, "../../../web/public/ads");
  await mkdir(webDir, { recursive: true });
  await run("cp", [finalOut, path.join(webDir, "itsbagelbot-zelios-16x9.mp4")]);
  console.log("wrote", finalOut);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
