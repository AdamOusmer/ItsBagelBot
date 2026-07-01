// App-wide toast store. Pages call toast(); the single <ToastHost> mounted in
// the app layout renders the stack. Undo-able toasts (e.g. optimistic deletes)
// carry an onUndo callback and live slightly longer.
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
const timers = new Map<number, ReturnType<typeof setTimeout>>();

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
  const timer = setTimeout(() => dismissToast(id), ttl);
  timers.set(id, timer);
  return id;
}

export function dismissToast(id: number): void {
  const timer = timers.get(id);
  if (timer) clearTimeout(timer);
  timers.delete(id);
  store.update((list) => list.filter((t) => t.id !== id));
}
