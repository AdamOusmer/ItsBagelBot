// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import type { Session } from './session';

export type ModuleLoadSpec<T> = {
  read: (uid: string) => Promise<T>;
  blank: () => T;
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
