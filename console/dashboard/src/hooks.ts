import type { Reroute } from '@sveltejs/kit';

// stats.itsbagelbot.com is this same app answering under a second hostname:
// its root serves the public stats page. This hook is universal — it runs on
// the server and in the client router alike, so a client-side navigation to
// '/' on the stats host (the language switch, for one) resolves to the same
// route the server rendered. Every other path falls through to the normal
// route table: /stats/* endpoints keep working, and authed routes on the
// stats host simply bounce to /login as they would anywhere.
//
// Matching the first label rather than a full hostname keeps local testing
// honest (stats.localhost) without a config knob.
export const reroute: Reroute = ({ url }) => {
  if (url.hostname.split('.')[0] === 'stats' && url.pathname === '/') return '/stats';
};
