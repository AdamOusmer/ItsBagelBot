// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
import { untrack } from 'svelte';
import type { SubmitFunction } from '@sveltejs/kit';
import {
  blankDiscordConfig,
  discordModuleKeys,
  fieldErrorsByField,
  flagValue,
  tDiscordModuleLabel,
  voiceCategoryMissing,
  type DiscordConfig,
  type DiscordModuleDef,
  type DiscordModuleSettings,
  type I18n,
  type RefusedFields
} from '@bagel/kit';
import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
import { toast } from '@bagel/ui/svelte/toast';
import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
import { fieldLabelOf, payloadOf, refusalTextOf, succeeded, type ActionPayload } from './action-result';

export type ModuleDraftData = {
  def: DiscordModuleDef;
  settings: DiscordModuleSettings;
  version: number;
  enabled: boolean;
};

type DraftInit = { data: () => ModuleDraftData; t: I18n['t'] };

function fullConfig(settings: DiscordModuleSettings): DiscordConfig {
  return { ...blankDiscordConfig(), ...settings };
}

function slice(config: DiscordConfig, keys: readonly (keyof DiscordConfig)[]): string {
  const out: Record<string, string> = {};
  for (const key of keys) out[key] = config[key];
  return JSON.stringify(out);
}

export function createModuleDraft(init: DraftInit) {
  const { t } = init;

  const seed = untrack(init.data);
  let seedId = seed.def.id;
  let config = $state<DiscordConfig>(fullConfig(seed.settings));
  let saved = $state.raw<DiscordConfig>(fullConfig(seed.settings));
  let version = $state<number>(seed.version ?? 0);
  let enabled = $state<boolean>(seed.enabled);
  let baseline = $state(slice(config, discordModuleKeys(seed.def)));
  let conflicted = $state(false);
  let busy = $state(false);
  let saveState = $state<SaveState>('idle');
  let toggleState = $state<SaveState>('idle');
  let invalidFields = $state<string[]>([]);

  const def = $derived(init.data().def);
  const keys = $derived(discordModuleKeys(def));
  const payload = $derived(slice(config, keys));
  const dirty = $derived(payload !== baseline);
  const invalid = $derived<RefusedFields>(fieldErrorsByField(invalidFields));
  const invalidBanner = $derived(bannerFor(invalid));

  $effect(() => {
    const next = init.data();
    untrack(() => reseed(next, false));
  });

  function reseed(next: ModuleDraftData, force: boolean) {
    const moved = next.def.id !== seedId;
    if (!force && !moved && dirty) return;
    seedId = next.def.id;
    config = fullConfig(next.settings);
    saved = fullConfig(next.settings);
    version = next.version ?? 0;
    enabled = next.enabled;
    baseline = slice(config, discordModuleKeys(next.def));
    invalidFields = [];
    conflicted = false;
  }

  const resetTimers: Record<string, ReturnType<typeof setTimeout> | undefined> = {};
  function mark(which: 'save' | 'toggle', s: SaveState, resetAfter = 0) {
    clearTimeout(resetTimers[which]);
    if (which === 'save') saveState = s;
    else toggleState = s;
    if (!resetAfter) return;
    resetTimers[which] = setTimeout(() => (which === 'save' ? (saveState = 'idle') : (toggleState = 'idle')), resetAfter);
  }

  const refusalText = (p: ActionPayload | undefined, fallback: string) => refusalTextOf(t, p, fallback, def.id);

  function bannerFor(map: RefusedFields): string {
    if (map.first !== '') return t('discord.invalidFieldBanner', { field: fieldLabelOf(t, def.id, map.first) });
    if (map.unknown.length > 0) return t('discord.errors.invalid');
    return '';
  }

  async function onSaved(p: ActionPayload | undefined) {
    mark('save', 'saved', 4000);
    invalidFields = [];
    conflicted = false;
    baseline = payload;
    if (typeof p?.version === 'number') version = p.version;
    toast('success', t('discord.toast.saved'));
    await invalidateAll();
  }

  function onSaveRefused(p: ActionPayload | undefined) {
    mark('save', 'error', 4000);
    conflicted = p?.code === 'conflict';
    invalidFields = p?.code === 'invalid' ? (p.fields ?? []) : [];
    toast('danger', refusalText(p, t('discord.toast.saveFailed')));
  }

  const blocksSave = () => def.id === 'voice' && voiceCategoryMissing(config);

  const saveSubmit: SubmitFunction = ({ cancel }) => {
    if (blocksSave()) {
      cancel();
      mark('save', 'error', 4000);
      toast('danger', t('discord.module.voiceCategoryRequired'));
      return;
    }
    mark('save', 'saving');
    return async ({ result }) => {
      const p = payloadOf(result);
      if (succeeded(result, p)) await onSaved(p);
      else onSaveRefused(p);
    };
  };

  const toggleSubmit: SubmitFunction = () => {
    const before = enabled;
    enabled = !before;
    mark('toggle', 'saving');
    return async ({ result }) => {
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        enabled = p?.enabled ?? enabled;
        if (typeof p?.version === 'number') version = p.version;
        mark('toggle', 'saved', 3000);
        return;
      }
      enabled = before;
      mark('toggle', 'error', 4000);
      conflicted = p?.code === 'conflict';
      toast('danger', refusalText(p, t('discord.module.toggleFailed', { label: tDiscordModuleLabel(t, def) })));
    };
  };

  function actionSubmit(okMsg: string, failMsg: string): SubmitFunction {
    return () => {
      busy = true;
      return async ({ result }) => {
        busy = false;
        const p = payloadOf(result);
        if (succeeded(result, p)) {
          toast('success', okMsg);
          return;
        }
        toast('danger', refusalText(p, failMsg));
      };
    };
  }

  async function reload() {
    await invalidateAll();
    reseed(init.data(), true);
  }

  const discard = createDiscardGuard(() => dirty, () => {
    baseline = payload;
  });
  beforeNavigate((nav) => {
    if (!dirty || !nav.to) return;
    nav.cancel();
    const href = nav.to.url.href;
    discard.guard(() => { void goto(href); });
  });

  return {
    get def() {
      return def;
    },
    get config() {
      return config;
    },
    get saved() {
      return saved;
    },
    get version() {
      return version;
    },
    get enabled() {
      return enabled;
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
    get saveState() {
      return saveState;
    },
    get toggleState() {
      return toggleState;
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
    toggleSubmit,
    reload,
    confirmDiscard: discard.confirm,
    cancelDiscard: discard.cancel
  };
}

export type ModuleDraft = ReturnType<typeof createModuleDraft>;
