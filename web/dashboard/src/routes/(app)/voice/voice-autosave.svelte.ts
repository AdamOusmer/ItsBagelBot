// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { postAction } from '#lib/client-actions.svelte';
import type { ActionOk } from '@bagel/kit';
import { toast } from '@bagel/ui/svelte/toast';
import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

export type VoiceSaveOutcome = 'saved' | 'conflict' | 'failed';
type WriteOutcome = VoiceSaveOutcome | 'dropped';
type SaveReply = ActionOk & { rev?: number; conflict?: boolean };

const SAVED_RESET_MS = 3000;
const ERROR_RESET_MS = 4000;
const DEFAULT_DEBOUNCE_MS = 700;

export class VoiceAutosave {
  status = $state<Record<string, SaveState>>({});
  revision = $state(0);

  #onConflict: () => Promise<void>;
  #text: { failed: () => string; conflict: () => string };
  #chain: Promise<unknown> = Promise.resolve();
  #generation = 0;
  #pending = new Map<string, { fields: Record<string, string>; timer: ReturnType<typeof setTimeout> }>();
  #resets = new Map<string, ReturnType<typeof setTimeout>>();

  constructor(options: {
    revision: number;
    onConflict: () => Promise<void>;
    text: { failed: () => string; conflict: () => string };
  }) {
    this.revision = options.revision;
    this.#onConflict = options.onConflict;
    this.#text = options.text;
  }

  #setStatus(key: string, state: SaveState, resetAfterMs?: number) {
    clearTimeout(this.#resets.get(key));
    this.#resets.delete(key);
    this.status = { ...this.status, [key]: state };
    if (resetAfterMs !== undefined) {
      this.#resets.set(key, setTimeout(() => (this.status = { ...this.status, [key]: 'idle' }), resetAfterMs));
    }
  }

  async #write(fields: Record<string, string>): Promise<WriteOutcome> {
    const body = new FormData();
    for (const [name, value] of Object.entries(fields)) body.set(name, value);
    body.set('expected_rev', String(this.revision));
    const payload = await postAction<SaveReply>('save', body);
    if (payload?.ok) {
      if (typeof payload.rev === 'number') this.revision = payload.rev;
      return 'saved';
    }
    if (payload?.conflict) {
      await this.#onConflict();
      toast('danger', this.#text.conflict());
      return 'conflict';
    }
    toast('danger', this.#text.failed());
    return 'failed';
  }

  #settle(key: string, outcome: WriteOutcome): VoiceSaveOutcome {
    if (outcome === 'saved') this.#setStatus(key, 'saved', SAVED_RESET_MS);
    else if (outcome === 'dropped') this.#setStatus(key, 'idle');
    else this.#setStatus(key, 'error', ERROR_RESET_MS);
    return outcome === 'dropped' ? 'conflict' : outcome;
  }

  save(key: string, fields: Record<string, string>): Promise<VoiceSaveOutcome> {
    this.#setStatus(key, 'saving');
    const generation = this.#generation;
    const result = this.#chain.then((): Promise<WriteOutcome> | WriteOutcome =>
      generation === this.#generation ? this.#write(fields) : 'dropped');
    this.#chain = result.catch(() => {});
    return result.then((outcome) => this.#settle(key, outcome));
  }

  schedule(key: string, fields: Record<string, string>, delayMs = DEFAULT_DEBOUNCE_MS): void {
    clearTimeout(this.#pending.get(key)?.timer);
    const timer = setTimeout(() => {
      this.#pending.delete(key);
      void this.save(key, fields);
    }, delayMs);
    this.#pending.set(key, { fields, timer });
  }

  async flush(): Promise<void> {
    const due = [...this.#pending];
    this.#pending.clear();
    for (const [key, { fields, timer }] of due) {
      clearTimeout(timer);
      void this.save(key, fields);
    }
    await this.#chain;
  }

  reset(revision: number): void {
    this.#generation += 1;
    for (const { timer } of this.#pending.values()) clearTimeout(timer);
    this.#pending.clear();
    this.revision = revision;
  }
}
