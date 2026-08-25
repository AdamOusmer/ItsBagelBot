<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Compact nested-game row on the loyalty page: name, one-liner, command
  // chips, a "Set up" link to the inspector, and a switch gated on loyalty.
  // Odds, limits and chat lines stay on /modules/[id] so this page does not
  // grow a second inspector.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Switch,
    ButtonLink,
    toast,
    getI18n,
    type ModuleDef
  } from '@bagel/shared';

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
        toast('err', t('loyalty.toastGameToggleFailed', { label: def.label }));
      }
    };
  };
</script>

<article class="game" class:on={enabled} id={def.id}>
  <span class="icon" aria-hidden="true"><Icon name={def.icon} size={18} /></span>
  <div class="copy">
    <span class="name">{def.label}</span>
    <span class="tagline">{def.tagline}</span>
    {#if chips.length}
      <span class="cmds">
        {#each chips as chip (chip)}
          <span class="cmd">{chip}</span>
        {/each}
      </span>
    {/if}
  </div>
  <div class="side">
    <ButtonLink {href} variant="ghost">{t('loyalty.gameSetup')}</ButtonLink>
    <form method="POST" action="?/toggleGame" use:enhance={submit}>
      <input type="hidden" name="name" value={def.id} />
      <input type="hidden" name="is_enabled" value={enabled ? '' : 'on'} />
      <Switch
        type="submit"
        checked={enabled}
        disabled={!loyaltyOn}
        pending={pending}
        label={enabled ? t('modules.disableAria', { label: def.label }) : t('modules.enableAria', { label: def.label })}
      />
    </form>
  </div>
</article>

<style>
  .game {
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    padding: 12px 0;
    border-bottom: 1px solid rgba(240, 236, 228, 0.05);
  }
  .game:last-child { border-bottom: none; padding-bottom: 0; }
  .game:first-child { padding-top: 0; }
  .game.on .icon {
    background: rgba(82, 183, 136, 0.12);
    border-color: rgba(82, 183, 136, 0.3);
    color: var(--bb-green-glow);
  }

  .icon {
    width: 40px;
    height: 40px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 8px;
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid var(--glass-border, var(--bb-border));
    color: var(--bb-tan-light);
    flex: none;
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
  .cmds { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
  .cmd {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    border: 1px solid rgba(201, 168, 124, 0.28);
    background: rgba(201, 168, 124, 0.08);
    border-radius: 6px;
    padding: 2px 7px;
    white-space: nowrap;
  }

  .side {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    flex: none;
  }

  @media (max-width: 560px) {
    .game { grid-template-columns: 40px minmax(0, 1fr); }
    .side { grid-column: 1 / -1; justify-content: flex-end; padding-top: 4px; }
  }
</style>
