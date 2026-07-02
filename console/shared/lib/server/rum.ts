// New Relic Browser (RUM) injection for streamed SSR, shared by both consoles.
//
// The agent loader must be inline in <head> and must carry the per-response CSP
// nonce SvelteKit emitted (script-src stays nonce-based, no 'unsafe-inline').
// Streaming constraint: the transform must never hold state ACROSS chunks — the
// previous implementation captured the nonce from "whichever chunk carries it",
// which serialized page flushes behind nonce capture. In SvelteKit's HTML shell
// the chunk containing </head> always also carries the nonce attributes, so we
// operate on that single chunk in one pass and pass every other chunk through
// untouched.
//
// Returns '' -> no-op when the agent is not connected (dev), when no nonce is
// present (CSP would block the inline script anyway), or after injection.
import newrelic from 'newrelic';

/** Per-request transformPageChunk. Construct one per handled request. */
export function rumTransform(): (opts: { html: string }) => string {
  let injected = false;
  return ({ html }) => {
    if (injected || !html.includes('</head>')) return html;
    injected = true;
    const nonce = html.match(/nonce="([^"]+)"/)?.[1];
    if (!nonce) return html;
    let snippet = '';
    try {
      snippet = newrelic.getBrowserTimingHeader({ nonce });
    } catch {
      /* agent not ready (e.g. dev); skip injection */
    }
    return snippet ? html.replace('</head>', `${snippet}</head>`) : html;
  };
}
