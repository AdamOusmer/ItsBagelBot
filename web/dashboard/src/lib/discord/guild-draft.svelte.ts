// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// One editable slice of a guild's config, plus everything that has to happen
// around saving it.
//
// This was the top third of the old single Discord page. Splitting that page
// into seven routes would otherwise have copied the draft, the version
// handling, the conflict path, the refused-field marks and the dirty guard into
// each of them; extracting the factory instead means every sub-page gets the
// identical behaviour and a bug is fixed once.
//
// `fields` is the slice the calling page owns. The payload carries only those
// keys, because `save` merges a PARTIAL draft (mergeDiscordConfig keeps the
// stored value of every key the patch does not mention) -- so two people
// editing two different sub-pages of the same server no longer overwrite each
// other with fields neither of them touched.
import { beforeNavigate, goto, invalidateAll } from '$app/navigation';
import { untrack } from 'svelte';
import type { SubmitFunction } from '@sveltejs/kit';
import {
  actionPayload,
  fieldErrorsByField,
  flagValue,
  toast,
  type ActionOk,
  type DiscordConfig,
  type I18n,
  type RefusedFields
} from '@bagel/shared';
import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';
import { DISCORD_CODE_KEYS } from '$lib/discord-messages';
import { FIELD_LABEL_KEYS } from './guild-fields';

/** The shape of the layout data a draft reads. Structural on purpose: the page
 *  props are generated per route and every one of them is a superset. */
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

/**
 * The refusal contract: switch on `code`, fall back to the sentence outgress
 * sent while it is still the only thing an older deployment returns.
 *
 * Module level rather than a method of the draft, because the pages that have
 * no draft at all -- Settings, and the Overview's module tiles -- still POST to
 * the same actions and must explain a refusal the same way. The tables live in
 * $lib/discord-messages so the server list and these pages cannot drift apart.
 */
export function refusalTextOf(t: I18n['t'], p: ActionPayload | undefined, fallback: string): string {
  if (p?.code === 'invalid' && p.fields?.length) {
    return t('discord.errInvalidFields', { fields: p.fields.map((f) => fieldLabelOf(t, f)).join(', ') });
  }
  const key = p?.code ? DISCORD_CODE_KEYS[p.code] : undefined;
  if (key) return t(key);
  // The translated sentence wins over `p.error`. Actions run on the server,
  // where there is no locale, so anything they phrase themselves is English for
  // every reader; the raw text is a last resort for a refusal this console has
  // no code for at all.
  return fallback || (p?.error ?? '');
}

type DraftInit = {
  /** Reactive read of the page data, so the draft reseeds when the load reruns. */
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

  // The seed is a plain reference, not state: it is the identity check that
  // says "the load reran", and making it reactive would make the reseed effect
  // depend on its own write.
  let seed = untrack(init.data);
  let config = $state<DiscordConfig>({ ...seed.config });
  let version = $state<number>(seed.version ?? 0);
  let baseline = $state(slice(seed.config, fields));
  let conflicted = $state(false);
  let busy = $state(false);
  let saveState = $state<SaveState>('idle');
  let saving = $state(false);
  let invalidFields = $state<string[]>([]);
  let pendingHref = $state('');
  let discardOpen = $state(false);
  let guarded = true;

  const payload = $derived(slice(config, fields));
  const dirty = $derived(payload !== baseline);
  const invalid = $derived<RefusedFields>(fieldErrorsByField(invalidFields));

  // Tracked read of the load's data, untracked write of the draft: the write
  // touches state this effect must not depend on, and a tracked write of state
  // the same effect reads is an unsafe cycle in Svelte 5.
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
    // A field this console has no control for: there is nowhere to put a mark,
    // so the banner is the whole notice.
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
    // An `invalid` refusal means the save DID land: every good field was
    // written and only the named ones kept their stored value. Reseeding is
    // what makes the refused control snap back to what is actually stored
    // instead of showing a draft the server rejected.
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

  // One factory instead of three near-identical closures: each one-shot action
  // differs only in which two strings it toasts.
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
        // A refused one-shot never redirects, so the guard has to come back on
        // or the next navigation drops the draft silently.
        guarded = true;
        toast('err', refusalText(p, failMsg));
      };
    };
  }

  async function reload() {
    conflicted = false;
    // The draft is abandoned deliberately: reseeding from the server is the
    // whole point of the button, and keeping the local edits would just
    // reproduce the conflict on the next save.
    baseline = payload;
    await invalidateAll();
  }

  /**
   * The dirty guard, and the one navigation it must not stop.
   *
   * Disconnecting redirects to /discord and there is nothing left to save --
   * the guild is unbound. Exempting the /discord PATH instead exempted the
   * server list too: clicking "Discord" in the nav with unsaved changes threw
   * them away silently, which is the exact case the guard exists for.
   */
  beforeNavigate((nav) => {
    if (!guarded) return;
    if (!dirty) return;
    if (!nav.to) return;
    if (pendingHref === nav.to.url.href) return;
    nav.cancel();
    pendingHref = nav.to.url.href;
    discardOpen = true;
  });

  function confirmDiscard() {
    discardOpen = false;
    // Baseline moves to the current draft so the second navigation is no longer
    // dirty and beforeNavigate lets it through.
    baseline = payload;
    if (pendingHref) goto(pendingHref);
  }

  function cancelDiscard() {
    discardOpen = false;
    pendingHref = '';
  }

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
      return discardOpen;
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
    confirmDiscard,
    cancelDiscard,
    /** Stand the guard down for exactly one navigation (the disconnect redirect). */
    releaseGuard() {
      guarded = false;
    },
    setBusy(v: boolean) {
      busy = v;
    }
  };
}

export type GuildDraft = ReturnType<typeof createGuildDraft>;
