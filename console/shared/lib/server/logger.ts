// Server-only structured logger, shared by both consoles.
//
// Why pino (not console.*): the New Relic Node agent auto-instruments pino and,
// with LOCAL_DECORATING_ENABLED, injects the NR-LINKING metadata into each JSON
// log line so the cluster Fluent Bit DaemonSet ships logs already correlated to
// APM traces (logs-in-context). Plain console.log/warn/error is NOT instrumented,
// so those lines reach New Relic undecorated. Every server-side log therefore
// goes through this singleton.
//
// Deliberately a bare `pino()` writing JSON to stdout — NO worker-thread transport
// (pino-pretty et al.). Transports spawn a worker thread that (a) does not survive
// the distroless adapter-node bundle and (b) moves log emission off the main thread
// where the agent's local-decoration hook cannot see it. Forwarding is off in the
// cluster (FORWARDING_ENABLED=false); the agent decorates, Fluent Bit ships stdout.
//
// LOG_LEVEL is read from `process.env` (safe at module eval, unlike SvelteKit's
// `$env/dynamic/private`, which deadlocks the boot import graph — see config.ts).
// adapter-node exposes the same values on process.env.
import pino from 'pino';

/** Process-wide structured logger. Level defaults to 'info' (override via LOG_LEVEL). */
export const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info'
});
