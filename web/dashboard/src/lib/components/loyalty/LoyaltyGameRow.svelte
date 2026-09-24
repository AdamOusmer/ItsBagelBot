<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Switch,
    toast,
    getI18n,
    tModuleLabel,
    tModuleTagline,
    type ModuleDef
  } from '@bagel/kit';

  let {
    def,
    enabled = $bindable(false),
    loyaltyOn
  }: {
    def: ModuleDef;
    enabled: boolean;
    loyaltyOn: boolean;
  } = $props();

  const { t } = getI18n();
  const href = $derived(def.href ?? `/modules/${def.id}`);
  const chips = $derived.by(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const command of def.commands ?? []) {
      const token = command.trigger.trim().split(/\s+/)[0] ?? '';
      const key = token.toLowerCase();
      if (!token || seen.has(key)) continue;
      seen.add(key);
      out.push(token);
      if (out.length >= 2) break;
    }
    return out;
  });
  let pending = $state(false);

  const submit: SubmitFunction = () => {
    if (!loyaltyOn) return;
    const was = enabled;
    enabled = !was;
    pending = true;
    return async ({ result }) => {
      pending = false;
      if (result.type !== 'success') {
        enabled = was;
        toast('err', t('loyalty.toastGameToggleFailed', { label: tModuleLabel(t, def) }));
      }
    };
  };
</script>

<article class="game" class:on={enabled} id={def.id}>
  <a class="main" {href} data-cursor="quiet">
    <span class="copy">
      <span class="name">{tModuleLabel(t, def)}</span>
      <span class="tagline">{tModuleTagline(t, def)}</span>
      {#if chips.length}
        <span class="cmds">
          {#each chips as chip (chip)}
            <span class="cmd bb-tag bb-tag--bare">{chip}</span>
          {/each}
        </span>
      {/if}
    </span>
  </a>
  <div class="side">
    <form method="POST" action="?/toggleGame" use:enhance={submit}>
      <input type="hidden" name="name" value={def.id} />
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <Switch
        type="submit"
        checked={enabled}
        disabled={!loyaltyOn}
        pending={pending}
        label={enabled ? t('modules.disableAria', { label: tModuleLabel(t, def) }) : t('modules.enableAria', { label: tModuleLabel(t, def) })}
      />
    </form>
  </div>
</article>

<style>
  .game {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid rgba(240, 236, 228, 0.05);
    isolation: isolate;
  }
  .game:last-child { border-bottom: none; }

  .main {
    flex: 1 1 auto;
    min-width: 0;
    display: block;
    padding: 12px 8px 12px 0;
    text-decoration: none;
    color: inherit;
    border-radius: var(--bb-radius-sm);
  }
  .main:hover { background: rgba(201, 168, 124, 0.05); }
  .main:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: -2px;
  }

  .copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
  }
  .tagline {
    font-family: var(--bb-font-body);
    font-size: 12px;
    line-height: 1.35;
    color: var(--bb-muted);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .cmds { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 6px; }
  .cmd { color: var(--bb-tan-light); text-transform: none; letter-spacing: 0.02em; }

  .side {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    flex: none;
    padding: 0 0 0 8px;
  }

  @media (max-width: 560px) {
    .game { flex-wrap: wrap; }
    .side { margin-left: auto; padding: 0 0 8px; }
  }
</style>
