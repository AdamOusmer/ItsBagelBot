// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { writable } from 'svelte/store';

export type ToastKind = 'ok' | 'err' | 'info';

export interface ToastItem {
  id: number;
  kind: ToastKind;
  text: string;
  undoLabel?: string;
  onUndo?: () => void;
}

const DEFAULT_TTL_MS = 3200;
const UNDO_TTL_MS = 5000;

const store = writable<ToastItem[]>([]);
export const toasts = { subscribe: store.subscribe };

let nextId = 1;
interface ToastTimer {
  handle?: ReturnType<typeof setTimeout>;
  remaining: number;
  startedAt: number;
}

const timers = new Map<number, ToastTimer>();

function arm(id: number, ms: number): void {
  timers.set(id, { handle: setTimeout(() => dismissToast(id), ms), remaining: ms, startedAt: Date.now() });
}

export interface ToastOptions {
  ttlMs?: number;
  undoLabel?: string;
  onUndo?: () => void;
}

export function toast(kind: ToastKind, text: string, opts: ToastOptions = {}): number {
  const id = nextId++;
  const item: ToastItem = { id, kind, text, undoLabel: opts.undoLabel, onUndo: opts.onUndo };
  store.update((list) => [...list, item]);
  const ttl = opts.ttlMs ?? (opts.onUndo ? UNDO_TTL_MS : DEFAULT_TTL_MS);
  arm(id, ttl);
  return id;
}

export function dismissToast(id: number): void {
  const timer = timers.get(id);
  if (timer?.handle) clearTimeout(timer.handle);
  timers.delete(id);
  store.update((list) => list.filter((t) => t.id !== id));
}

export function pauseToast(id: number): void {
  const timer = timers.get(id);
  if (!timer?.handle) return;
  clearTimeout(timer.handle);
  const remaining = Math.max(0, timer.remaining - (Date.now() - timer.startedAt));
  timers.set(id, { remaining, startedAt: 0 });
}

export function resumeToast(id: number): void {
  const timer = timers.get(id);
  if (!timer || timer.handle) return;
  arm(id, timer.remaining);
}
