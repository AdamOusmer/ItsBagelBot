// New Relic Browser (RUM) injection — DISABLED, shared by both consoles.
//
// This used to inject the New Relic Browser agent (a ~69KB inline <script> from
// newrelic.getBrowserTimingHeader) into the <head> of every SSR HTML page. It was
// removed because:
//   * getBrowserTimingHeader sat on the streamed response's first-chunk critical
//     path and added ~2s to EVERY full-page render, so every page in both
//     consoles (not just any one route) was slow;
//   * it shipped a third-party browser agent that stored a client-side session id
//     and reported user data to New Relic with no consent gate — a privacy/ePrivacy
//     concern for EU (fr) visitors.
// Server-side APM is unaffected; only the browser-side agent is gone.
//
// rumTransform is kept as a no-op passthrough so the hooks.server.ts wiring in
// both apps stays unchanged. To re-enable RUM: restore the getBrowserTimingHeader
// injection (cache the static loader once per process and splice only the
// per-request CSP nonce in, so it never re-blocks the render) AND add a consent
// gate before the agent ever reaches the browser.

/** Per-request transformPageChunk. No-op: RUM injection is disabled. */
export function rumTransform(): (opts: { html: string }) => string {
  return ({ html }) => html;
}
