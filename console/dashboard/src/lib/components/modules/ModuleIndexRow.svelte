<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One index row: the name/purpose/commands are a real link (open the module),
  // the switch is a sibling so it is never nested inside that link. Streamers
  // recognise a module by the command chat types, so chips sit in the same
  // glance as the name rather than behind a "Configure" button.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, SaveStatus, Switch, getI18n, moduleCommandChips, moduleHref, type ModuleState } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    module,
    status = 'idle' as SaveState,
    toggleSubmit
  }: {
    module: ModuleState;
    status?: SaveState;
    toggleSubmit: SubmitFunction;
  } = $props();

  const def = $derived(module.def);
  const href = $derived(moduleHref(def));
  const chips = $derived(moduleCommandChips(def));
  const toggleable = $derived(def.toggleable !== false);
  // Beta chip shows for everyone (premium included: it sets expectations);
  // the lock replaces the switch only when the board is not premium.
  const beta = $derived(def.beta === true);
  const locked = $derived(module.locked === true);
</script>

<article class="mod" class:on={module.enabled && !locked} class:off={!module.enabled || locked} class:locked>
  <!-- data-cursor="off": a row is a reading surface, not a control. The
       custom cursor morphs onto any <a>, and filling this whole card with a
       tan box covered the switch and read against the dock. -->
  <a class="main" {href} data-cursor="off">
    <span class="icon" aria-hidden="true"><Icon name={def.icon} size={18} /></span>
    <span class="copy">
      <span class="name">
        {def.label}
        {#if beta}<span class="beta">{t('modules.betaChip')}</span>{/if}
      </span>
      <span class="tagline">{def.tagline}</span>
      {#if chips.chips.length}
        <span class="cmds">
          {#each chips.chips as chip (chip)}
            <span class="cmd">{chip}</span>
          {/each}
          {#if chips.extra}
            <span class="cmd more">{t('modules.moreCommands', { n: chips.extra })}</span>
          {/if}
        </span>
      {/if}
    </span>
  </a>
  <div class="side">
    <SaveStatus state={status} compact />
    {#if locked}
      <a class="always lock" href="/billing" data-cursor="off"><Icon name="gem" size={12} /> {t('modules.betaPremium')}</a>
    {:else if toggleable}
      {#if module.enabled}
        <span class="state live">{t('modules.statusOn')}</span>
      {/if}
      <form method="POST" action="?/toggle" use:enhance={toggleSubmit}>
        <input type="hidden" name="name" value={def.id} />
        <input type="hidden" name="is_enabled" value={module.enabled ? '' : 'on'} />
        <Switch
          type="submit"
          checked={module.enabled}
          label={module.enabled ? t('modules.disableAria', { label: def.label }) : t('modules.enableAria', { label: def.label })}
          pending={status === 'saving'}
        />
      </form>
    {:else}
      <span class="always">{t('modules.alwaysOn')}</span>
    {/if}
  </div>
</article>

<style>
  .mod {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid var(--rule);
    isolation: isolate;
  }
  .mod:last-child { border-bottom: none; }
  .mod.on { background: linear-gradient(90deg, rgba(82, 183, 136, 0.07), transparent 42%); }

  .main {
    flex: 1 1 auto;
    min-width: 0;
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr);
    align-items: start;
    gap: 14px;
    padding: 14px 12px 14px 16px;
    text-decoration: none;
    color: inherit;
    position: relative;
  }
  .main::before {
    content: "";
    position: absolute;
    left: 0;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 2px;
    background: var(--rule);
  }
  .on .main::before {
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .main:hover { background: rgba(201, 168, 124, 0.05); }
  .main:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: -2px;
  }

  .icon {
    width: 40px;
    height: 40px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 8px;
    background: rgba(201, 168, 124, 0.1);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    flex: none;
  }
  .on .icon {
    background: rgba(82, 183, 136, 0.12);
    border-color: rgba(82, 183, 136, 0.3);
    color: var(--bb-green-glow);
  }

  .copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: -0.015em;
    color: var(--bb-white);
    line-height: 1.2;
  }
  .tagline {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.4;
    color: var(--bb-muted);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .cmds {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
  }
  .cmd {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    border: 1px solid rgba(201, 168, 124, 0.28);
    background: rgba(201, 168, 124, 0.08);
    border-radius: 6px;
    padding: 3px 8px;
    white-space: nowrap;
  }
  .cmd.more { color: var(--bb-muted); border-color: var(--rule); background: transparent; }

  /* Filled rather than a hairline outline, and a size up: at 9.5px with a
     45%-alpha border this read as decoration and people missed that the
     module was gated at all. The label carries "Premium" too, so the chip
     answers "why can I not turn this on" without a hover or a click. */
  .beta {
    display: inline-block;
    vertical-align: 1px;
    margin-left: 8px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-ink, #1b1409);
    background: var(--bb-tan-light);
    border: 1px solid var(--bb-tan-light);
    border-radius: 5px;
    padding: 2px 7px;
    line-height: 1.35;
  }
  .locked .icon { opacity: 0.6; }

  .side {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    padding: 0 16px 0 8px;
    flex: none;
  }
  .state {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .state.live { color: var(--bb-green-glow); }
  .always {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    white-space: nowrap;
  }
  .lock {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    text-decoration: none;
    color: var(--bb-tan-light);
  }
  .lock:hover { text-decoration: underline; }

  @media (max-width: 760px) {
    .main { gap: 12px; }
    .side { padding-right: 10px; padding-left: 0; }
  }
</style>
