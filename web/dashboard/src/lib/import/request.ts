// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { deserialize } from '$app/forms';
import { classifyFailure, offlineFailure, type ImportFailure } from './errors';

export const IMPORT_TIMEOUT_MS = 30_000;

export type ActionOutcome =
  | { kind: 'success'; data: Record<string, unknown> }
  | { kind: 'failure'; failure: ImportFailure; message?: string }
  | { kind: 'cancelled' };

type Abort = { timedOut: boolean };

function failureOf(status: number | undefined, data: unknown): ActionOutcome {
  const d = (data ?? {}) as { error?: string; code?: string };
  return { kind: 'failure', failure: classifyFailure({ status, code: d.code }), message: d.error };
}

function interpret(result: ReturnType<typeof deserialize>): ActionOutcome {
  if (result.type === 'success') return { kind: 'success', data: (result.data ?? {}) as Record<string, unknown> };
  if (result.type === 'failure') return failureOf(result.status, result.data);
  return { kind: 'failure', failure: 'generic' };
}

function interrupted(abort: Abort): ActionOutcome {
  if (abort.timedOut) return { kind: 'failure', failure: classifyFailure({ aborted: 'timeout' }) };
  return { kind: 'cancelled' };
}

export async function postImportAction(
  action: 'preview' | 'commit',
  body: FormData,
  controller: AbortController,
  timeoutMs: number = IMPORT_TIMEOUT_MS
): Promise<ActionOutcome> {
  const abort: Abort = { timedOut: false };
  const timer = setTimeout(() => {
    abort.timedOut = true;
    controller.abort();
  }, timeoutMs);
  try {
    const res = await fetch(`/settings/import?/${action}`, { method: 'POST', body, signal: controller.signal });
    return interpret(deserialize(await res.text()));
  } catch {
    if (controller.signal.aborted) return interrupted(abort);
    return { kind: 'failure', failure: offlineFailure(typeof navigator === 'undefined' || navigator.onLine) };
  } finally {
    clearTimeout(timer);
  }
}
