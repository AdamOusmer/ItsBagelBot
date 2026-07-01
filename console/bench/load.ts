// Tiny latency bench for the p99 ≤ 200ms target. Fires `total` requests at a
// fixed `concurrency` against each path and reports p50/p90/p99/max.
//
//   bun run console/bench/load.ts [baseURL] [concurrency] [total]
//
// Defaults hit the local prod build. Run against a server wired to a live NATS
// (not DEMO) for representative SSR+RPC numbers; DEMO mode measures render-only.
const base = process.argv[2] ?? 'http://localhost:3000';
const concurrency = Number(process.argv[3] ?? 50);
const total = Number(process.argv[4] ?? 2000);
// Current dashboard route set (/moderation never shipped; settings + modules
// exercise the delegation and projection read paths respectively).
const paths = ['/', '/commands', '/modules', '/settings'];

function pct(sorted: number[], p: number): number {
  if (!sorted.length) return 0;
  const i = Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1);
  return sorted[i];
}

async function run(path: string) {
  const url = base + path;
  const lat: number[] = [];
  let errors = 0;
  let sent = 0;

  async function worker() {
    while (sent < total) {
      sent++;
      const t0 = performance.now();
      try {
        const res = await fetch(url, { redirect: 'manual' });
        await res.arrayBuffer();
        if (res.status >= 500) errors++;
      } catch {
        errors++;
      }
      lat.push(performance.now() - t0);
    }
  }

  const t0 = performance.now();
  await Promise.all(Array.from({ length: concurrency }, worker));
  const wall = (performance.now() - t0) / 1000;
  lat.sort((a, b) => a - b);

  const p99 = pct(lat, 99);
  console.log(
    `${path.padEnd(12)} n=${lat.length} rps=${(lat.length / wall).toFixed(0)} ` +
      `p50=${pct(lat, 50).toFixed(1)}ms p90=${pct(lat, 90).toFixed(1)}ms ` +
      `p99=${p99.toFixed(1)}ms max=${lat[lat.length - 1].toFixed(1)}ms err=${errors} ` +
      `${p99 <= 200 ? 'PASS' : 'FAIL'}`
  );
}

console.log(`bench ${base} c=${concurrency} n=${total}/path`);
for (const p of paths) await run(p);
