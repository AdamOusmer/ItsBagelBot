// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The load prelude every bespoke module page repeats: delegate gate, resolve
// the board being edited, short-circuit demo, then read with a degraded
// fallback.
//
// Template Method: the skeleton and its ORDER are fixed here, the three varying
// steps are hooks. The order is the part that had already drifted between
// copies and is worth pinning:
//   1. the gate runs FIRST, before demo and before any read, so a delegate
//      without the module's section is bounced rather than served a page whose
//      body then fails open;
//   2. `effectiveId` (not the session's own id) is what a read is keyed by, so
//      a delegate edits the OWNER's board;
//   3. a failed read degrades to `blank() + degraded: true` rather than
//      throwing, because a module page that 500s hides the rest of the console
//      shell -- the pages render a degraded banner instead.
//
// `demo` is a thunk supplied by the caller rather than a branch here: the
// canonical build-time demo gate and its dynamic `import('$lib/server/demo-data')`
// must sit in the same file for Rollup to erase them from a production build
// (see shared/scripts/assert-demo-gated.mjs, rule 4). Callers pass
// `DEMO ? () => … : undefined`, which folds to `undefined` and takes the
// fixture import edge with it.
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import type { Session } from './session';

export type ModuleLoadSpec<T> = {
  /** Read the page view for the board being edited. */
  read: (uid: string) => Promise<T>;
  /** The empty view a failed read degrades to. */
  blank: () => T;
  /** Demo fixture view; only ever set in a `vite dev` demo build. */
  demo?: () => Promise<T>;
};

export async function moduleLoad<T>(
  modId: string,
  session: Session | null | undefined,
  spec: ModuleLoadSpec<T>
): Promise<T | (T & { degraded: true })> {
  gateModulePage(session, modId);
  if (spec.demo) return spec.demo();
  try {
    return await spec.read(effectiveId(session));
  } catch {
    return { ...spec.blank(), degraded: true };
  }
}
