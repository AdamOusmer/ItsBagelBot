// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ActionOk = { ok?: boolean; error?: string };

export function actionPayload<T = ActionOk>(result: unknown): T | undefined {
  const r = result as { type?: string; data?: T } | null;
  return r?.type === 'success' || r?.type === 'failure' ? r.data : undefined;
}

export function toastFailure(
  toast: (kind: 'err', text: string) => unknown,
  t: (key: string) => string
): (payload: ActionOk | null | undefined, fallbackKey: string) => void {
  return (payload, fallbackKey) => {
    toast('err', payload?.error ?? t(fallbackKey));
  };
}

export type AdminActionOk = ActionOk & { notice?: string; action?: { ok?: boolean; notice?: string } };

export function adminToastFailure(
  toast: (kind: 'err', text: string) => unknown
): (payload: AdminActionOk | null | undefined, fallback: string) => void {
  return (payload, fallback) => {
    toast('err', payload?.action?.notice ?? payload?.notice ?? payload?.error ?? fallback);
  };
}
