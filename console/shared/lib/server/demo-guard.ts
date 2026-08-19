// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Runtime backstop for demo mode, shared by both consoles.
//
// Demo mode is a build-time property: every branch is gated on SvelteKit's
// `dev` constant, so a production build carries no demo code at all, and
// scripts/assert-demo-gated.mjs + scripts/assert-production-clean.ts fail the
// build if one ever survives. A DEMO that reaches a production image is
// therefore inert — which is exactly why it must not be ignored: it means an
// operator believes the pod is a demo instance when it cannot be one.
//
// The question asked here is deliberately broader than enablement. Turning
// demo ON takes exactly DEMO=1, so DEMO=true does nothing at all; on a build
// with demo compiled out, that silence is the problem, not the value. Only an
// explicit off ('0', 'false', 'off', 'no', empty) passes.
//
// The key is looked up rather than read as `env.DEMO` on purpose. Both
// consoles' build scans fail on any `env.DEMO` text left in the emitted
// bundle, since a surviving read means a demo branch escaped dead-code
// elimination. This deliberate check must not be the thing that trips them,
// and a property read written any other way (bracket access, a folded string
// concatenation) is rewritten back to `env.DEMO` by terser.
const EXPLICIT_OFF = ['0', 'false', 'off', 'no'];

export function demoConfigured(env: Record<string, string | undefined>): boolean {
  return Object.entries(env).some(
    ([key, value]) => key === 'DEMO' && !!value && !EXPLICIT_OFF.includes(value.toLowerCase())
  );
}
