<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import {
    getI18n,
    moduleCommandChips,
    moduleHref,
    tModuleLabel,
    tModuleTagline,
    type ModuleState
  } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

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
  const beta = $derived(def.beta === true);
  const locked = $derived(module.locked === true);
</script>

<article class="mrow" class:mrow--on={module.enabled}>
  <a class="mrow__main" {href} aria-label="{t('modules.openSettings')}: {tModuleLabel(t, def)}">
    <span class="mrow__name">
      {tModuleLabel(t, def)}
      {#if beta}<span class="mrow__beta"><Tag tone="alpha">{t('modules.betaChip')}</Tag></span>{/if}
    </span>
    <span class="mrow__tagline">{tModuleTagline(t, def)}</span>
  </a>
  {#if chips.chips.length}
    <span class="mrow__cmds" aria-hidden="true">
      {#each chips.chips as chip (chip)}
        <code class="mrow__cmd">{chip}</code>
      {/each}
      {#if chips.extra}
        <code class="mrow__cmd mrow__cmd--more">{t('modules.moreCommands', { n: chips.extra })}</code>
      {/if}
    </span>
  {/if}
  <span class="mrow__side">
    <SaveStatus state={status} compact />
    {#if locked}
      <TextLink href="/billing" label={t('modules.betaPremium')} />
    {:else if toggleable}
      <form method="POST" action="?/toggle" use:enhance={toggleSubmit}>
        <input type="hidden" name="name" value={def.id} />
        <input type="hidden" name="is_enabled" value={module.enabled ? '' : 'on'} />
        <Switch
          type="submit"
          checked={module.enabled}
          label={module.enabled ? t('modules.disableAria', { label: tModuleLabel(t, def) }) : t('modules.enableAria', { label: tModuleLabel(t, def) })}
          busy={status === 'saving'}
        />
      </form>
    {:else}
      <Tag tone="pre" data-fit-box><span data-fit="">{t('modules.alwaysOn')}</span></Tag>
    {/if}
  </span>
</article>

<style>
  .mrow {
    display: grid;
    grid-template-areas: 'main cmds side';
    grid-template-columns: minmax(0, 1fr) minmax(0, 15rem) 8.5rem;
    align-items: center;
    gap: var(--bb-space-4);
    min-height: 68px;
    padding: var(--bb-space-2) var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .mrow:hover {
    background: var(--bb-elev-2);
    box-shadow: var(--bb-depth-1);
  }

  .mrow__main {
    grid-area: main;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }

  .mrow__main:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: 4px;
    border-radius: var(--bb-radius-xs);
  }

  .mrow__name {
    font-weight: 600;
    color: var(--bb-paper);
    transition: color var(--bb-dur-fast) ease;
  }

  .mrow--on .mrow__name { color: var(--bb-tier-free); }
  .mrow__main:hover .mrow__name { color: var(--bb-tan-light); }

  .mrow__beta {
    display: inline-flex;
    margin-left: var(--bb-space-2);
    vertical-align: 1px;
  }

  .mrow__tagline {
    overflow: hidden;
    font-size: var(--bb-text-sm);
    color: var(--bb-muted-light);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mrow__cmds {
    grid-area: cmds;
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--bb-space-1);
  }

  .mrow__cmd {
    padding: 2px 6px;
    border-radius: var(--bb-radius-xs);
    background: rgba(var(--bb-white-rgb), 0.06);
    font: var(--bb-text-xs)/1.4 var(--bb-font-mono);
    color: var(--bb-tan-light);
  }

  .mrow__cmd--more { color: var(--bb-muted); }

  .mrow__side {
    grid-area: side;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--bb-space-3);
  }

  @media (max-width: 760px) {
    .mrow {
      grid-template-areas: 'main side';
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .mrow__cmds { display: none; }
  }
</style>
