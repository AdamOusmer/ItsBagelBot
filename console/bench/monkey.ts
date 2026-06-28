// Crazy Monkey Test - pushes the system to its limits with randomized chaos
// It calculates the overall p99 across all randomized requests.
//
// Usage: bun run console/bench/monkey.ts [baseURL] [concurrency] [total]

const base = process.argv[2] ?? 'http://localhost:3000';
const concurrency = Number(process.argv[3] ?? 500); // Pushed concurrency
const total = Number(process.argv[4] ?? 20000); // Pushed total

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'OPTIONS', 'HEAD'];

// Mix of normal paths, weird characters, large query parameters, and exploits
const paths = [
  '/',
  '/commands',
  '/moderation',
  '/api/v1/unknown',
  '//',
  '/../../etc/passwd',
  '/commands?foo=bar&baz=1',
  '/moderation/123/delete',
  `/${Math.random().toString(36).substring(7)}`,
  '/?q=' + 'A'.repeat(5000), // huge query string
  '/%00',
  '/%ff%fe%20%20',
  '/admin',
  '/.env'
];

const bodies = [
  null,
  '{}',
  '{"foo":"bar"}',
  'A'.repeat(50000), // huge body
  '<xml><invalid>',
  'DROP TABLE users;',
  '{"query": "mutation { dropAll }"}',
  Buffer.alloc(1024 * 1024).toString() // 1MB body
];

const headersList = [
  {},
  { 'Content-Type': 'application/json' },
  { 'Content-Type': 'application/xml', 'Accept': 'text/html' },
  { 'X-Forwarded-For': '127.0.0.1' },
  { 'User-Agent': 'MonkeyTest/1.0 Chaos/99' },
  { 'Authorization': 'Bearer invalid_token' },
  { 'Cookie': 'session_id=12345; user=admin' },
  { 'X-Crazy-Header': 'A'.repeat(10000) }, // large header
  { 'Origin': 'http://evil.com' },
];

function pct(sorted: number[], p: number): number {
  if (!sorted.length) return 0;
  const i = Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1);
  return sorted[i];
}

function rand<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)];
}

async function run() {
  const lat: number[] = [];
  let errors = 0;
  let sent = 0;

  async function worker() {
    while (sent < total) {
      sent++;
      const path = rand(paths);
      const url = base + path;
      const method = rand(methods);
      // fetch doesn't allow body with GET/HEAD
      const body = (method === 'GET' || method === 'HEAD') ? null : rand(bodies);
      const headers = rand(headersList);

      const t0 = performance.now();
      try {
        const res = await fetch(url, {
          method,
          body,
          headers: headers as any,
          redirect: 'manual'
        });
        await res.arrayBuffer(); // read out the body
        if (res.status >= 500) errors++; // track server errors
      } catch (err) {
        // Connection refused, timeout, or parse error
        errors++;
      }
      lat.push(performance.now() - t0);
    }
  }

  const t0 = performance.now();
  // Spawn the workers
  await Promise.all(Array.from({ length: concurrency }, worker));
  const wall = (performance.now() - t0) / 1000;

  // Sort for percentiles
  lat.sort((a, b) => a - b);

  const p50 = pct(lat, 50);
  const p90 = pct(lat, 90);
  const p99 = pct(lat, 99); // Full system p99
  const max = lat[lat.length - 1];

  console.log(`\n=== 🦧 CRAZY MONKEY TEST RESULTS 🦧 ===`);
  console.log(`Base URL:     ${base}`);
  console.log(`Concurrency:  ${concurrency}`);
  console.log(`Total Req:    ${total}`);
  console.log(`Wall Time:    ${wall.toFixed(2)}s`);
  console.log(`RPS:          ${(lat.length / wall).toFixed(0)}`);
  console.log(`Errors:       ${errors} (${((errors/total)*100).toFixed(1)}%)`);
  console.log(`-------------------------------------`);
  console.log(`Global p50:   ${p50.toFixed(1)}ms`);
  console.log(`Global p90:   ${p90.toFixed(1)}ms`);
  console.log(`Global p99:   ${p99.toFixed(1)}ms  <-- Full System p99`);
  console.log(`Global Max:   ${max.toFixed(1)}ms`);
  console.log(`=====================================\n`);
}

console.log(`Waking up the monkeys... 🌴 c=${concurrency} n=${total}`);
run().catch(console.error);
