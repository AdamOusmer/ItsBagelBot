// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
import { untrack } from 'svelte';
import type { SubmitFunction } from '@sveltejs/kit';
import {
  actionPayload,
  createDiscardGuard,
  fieldErrorsByField,
  flagValue,
  toast,
  type ActionOk,
  type DiscordConfig,
  type I18n,
  type RefusedFields
} from '@bagel/kit';
import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
import { DISCORD_CODE_KEYS } from '$lib/discord-messages';
import { FIELD_LABEL_KEYS } from './guild-fields';

export type GuildDraftData = { config: DiscordConfig; version: number };

export type ActionPayload = ActionOk & {
  code?: string;
  refused?: string;
  fields?: string[];
};

export function payloadOf(result: unknown): ActionPayload | undefined {
  return actionPayload<ActionPayload>(result);
}

export function succeeded(result: { type: string }, p: ActionPayload | undefined): boolean {
  return result.type === 'success' && p?.ok !== false;
}

export function fieldLabelOf(t: I18n['t'], field: string): string {
  const key = FIELD_LABEL_KEYS[field as keyof DiscordConfig];
  return key ? t(key) : field;
}

export function refusalTextOf(t: I18n['t'], p: ActionPayload | undefined, fallback: string): string {
  if (p?.code === 'invalid' && p.fields?.length) {
    return t('discord.errInvalidFields', { fields: p.fields.map((f) => fieldLabelOf(t, f)).join(', ') });
  }
  const key = p?.code ? DISCORD_CODE_KEYS[p.code] : undefined;
  if (key) return t(key);
  return fallback || (p?.error ?? '');
}

type DraftInit = {
  data: () => GuildDraftData;
  fields: readonly (keyof DiscordConfig)[];
  t: I18n['t'];
};

function pick(config: DiscordConfig, fields: readonly (keyof DiscordConfig)[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of fields) out[f] = config[f];
  return out;
}

function slice(config: DiscordConfig, fields: readonly (keyof DiscordConfig)[]): string {
  return JSON.stringify(pick(config, fields));
}

export function createGuildDraft(init: DraftInit) {
  const { t, fields } = init;

  let seed = untrack(init.data);
  let config = $state<DiscordConfig>({ ...seed.config });
  let version = $state<number>(seed.version ?? 0);
  let baseline = $state(slice(seed.config, fields));
  let conflicted = $state(false);
  let busy = $state(false);
  let saveState = $state<SaveState>('idle');
  let saving = $state(false);
  let invalidFields = $state<string[]>([]);
  let guarded = true;

  const payload = $derived(slice(config, fields));
  const dirty = $derived(payload !== baseline);
  const invalid = $derived<RefusedFields>(fieldErrorsByField(invalidFields));

  $effect(() => {
    const next = init.data();
    untrack(() => reseed(next));
  });

  function reseed(next: GuildDraftData) {
    if (next === seed) return;
    seed = next;
    config = { ...next.config };
    version = next.version ?? 0;
    baseline = slice(next.config, fields);
  }

  let resetTimer: ReturnType<typeof setTimeout> | undefined;
  function markSave(s: SaveState, resetAfter = 0) {
    clearTimeout(resetTimer);
    saveState = s;
    if (resetAfter) resetTimer = setTimeout(() => (saveState = 'idle'), resetAfter);
  }

  const fieldLabel = (field: string) => fieldLabelOf(t, field);
  const refusalText = (p: ActionPayload | undefined, fallback: string) => refusalTextOf(t, p, fallback);

  const invalidBanner = $derived(bannerFor(invalid));
  function bannerFor(map: RefusedFields): string {
    if (map.first !== '') return t('discord.invalidFieldBanner', { field: fieldLabel(map.first) });
    if (map.unknown.length > 0) return t('discord.errInvalid');
    return '';
  }

  async function onSaved() {
    markSave('saved', 4000);
    invalidFields = [];
    conflicted = false;
    toast('ok', t('discord.toastSaved'));
    await invalidateAll();
  }

  async function onSaveRefused(p: ActionPayload | undefined) {
    markSave('error', 4000);
    conflicted = p?.code === 'conflict';
    invalidFields = p?.code === 'invalid' ? (p.fields ?? []) : [];
    toast('err', refusalText(p, t('discord.toastSaveFailed')));
    if (p?.code === 'invalid') await invalidateAll();
  }

  const saveSubmit: SubmitFunction = () => {
    saving = true;
    markSave('saving');
    return async ({ result }) => {
      saving = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) await onSaved();
      else await onSaveRefused(p);
    };
  };

  function actionSubmit(okMsg: string, failMsg: string): SubmitFunction {
    return () => {
      busy = true;
      return async ({ result }) => {
        busy = false;
        const p = payloadOf(result);
        if (succeeded(result, p)) {
          toast('ok', okMsg);
          await invalidateAll();
          return;
        }
        guarded = true;
        toast('err', refusalText(p, failMsg));
      };
    };
  }

  async function reload() {
    conflicted = false;
    baseline = payload;
    await invalidateAll();
  }

  const discard = createDiscardGuard(() => dirty, () => {
    baseline = payload;
  });
  beforeNavigate((nav) => {
    if (!guarded || !dirty || !nav.to) return;
    nav.cancel();
    const href = nav.to.url.href;
    discard.guard(() => { void goto(href); });
  });

  return {
    get config() {
      return config;
    },
    get version() {
      return version;
    },
    get payload() {
      return payload;
    },
    get dirty() {
      return dirty;
    },
    get busy() {
      return busy;
    },
    get saving() {
      return saving;
    },
    get saveState() {
      return saveState;
    },
    get conflicted() {
      return conflicted;
    },
    get invalid() {
      return invalid;
    },
    get invalidBanner() {
      return invalidBanner;
    },
    get discardOpen() {
      return discard.open;
    },
    set(field: keyof DiscordConfig, value: string) {
      config[field] = value;
    },
    setFlag(field: keyof DiscordConfig, on: boolean) {
      config[field] = flagValue(on);
    },
    refusalText,
    actionSubmit,
    saveSubmit,
    reload,
    confirmDiscard: discard.confirm,
    cancelDiscard: discard.cancel,
    releaseGuard() {
      guarded = false;
    },
    setBusy(v: boolean) {
      busy = v;
    }
  };
}

export type GuildDraft = ReturnType<typeof createGuildDraft>;
