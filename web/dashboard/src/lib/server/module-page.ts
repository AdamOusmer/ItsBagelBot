// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import type { Session } from './session';

export type ModuleLoadSpec<T, C = undefined> = {
  context?: () => Promise<C>;
  read: (uid: string, context: C) => Promise<T>;
  blank: (context: C | undefined) => T;
  demo?: () => Promise<T>;
};

export async function moduleLoad<T, C = undefined>(
  modId: string,
  session: Session | null | undefined,
  spec: ModuleLoadSpec<T, C>
): Promise<T | (T & { degraded: true })> {
  gateModulePage(session, modId);
  if (spec.demo) return spec.demo();
  let context: C | undefined;
  try {
    context = await spec.context?.();
    return await spec.read(effectiveId(session), context as C);
  } catch {
    return { ...spec.blank(context), degraded: true };
  }
}
