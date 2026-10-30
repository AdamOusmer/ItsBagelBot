<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n, moduleHref, tModuleLabel, type ModuleState } from '@bagel/kit';
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
  const toggleable = $derived(def.toggleable !== false);
  const beta = $derived(def.beta === true);
  const locked = $derived(module.locked === true);
</script>

<article class="mrow" class:mrow--on={module.enabled}>
  <a class="mrow__main" {href} aria-label="{t('modules.openSettings')}: {tModuleLabel(t, def)}">
    <span class="mrow__label">
      <span class="mrow__name">{tModuleLabel(t, def)}</span>
      {#if beta}<span class="mrow__beta"><Tag tone="alpha">{t('modules.betaChip')}</Tag></span>{/if}
    </span>
    <span class="mrow__go" aria-hidden="true">
      <span class="mrow__go-text" data-fit-box><span data-fit="">{t('modules.openSettings')}</span></span>
      <Icon name="chevron" size={14} class="mrow__chev" />
    </span>
  </a>
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
    grid-template-columns: minmax(0, 1fr) 8.5rem;
    align-items: center;
    gap: var(--bb-space-4);
    min-height: 56px;
    padding: 0 var(--bb-space-3) 0 0;
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease, box-shadow var(--bb-dur-fast) ease;
  }

  .mrow:hover {
    background: var(--bb-elev-2);
    box-shadow: var(--bb-depth-1);
  }

  .mrow__main {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 9.5rem;
    align-items: center;
    gap: var(--bb-space-4);
    align-self: stretch;
    min-width: 0;
    padding: var(--bb-space-2) 0 var(--bb-space-2) var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    color: inherit;
    text-decoration: none;
  }

  .mrow__main:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: 2px;
  }

  .mrow__label {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-width: 0;
  }

  .mrow__name {
    overflow: hidden;
    font-weight: 600;
    color: var(--bb-tier-inactive);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mrow--on .mrow__name { color: var(--bb-tier-free); }

  .mrow__beta { display: inline-flex; }

  .mrow__go {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--bb-space-1);
    font-size: var(--bb-text-sm);
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease;
  }

  .mrow__go-text {
    display: block;
    width: 8rem;
    overflow: hidden;
    text-align: right;
    white-space: nowrap;
  }

  .mrow__go :global(.mrow__chev) {
    flex: none;
    transform: rotate(-90deg);
  }

  .mrow:hover .mrow__go {
    color: var(--bb-tan-light);
  }

  .mrow__side {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--bb-space-3);
  }

  @media (max-width: 760px) {
    .mrow { grid-template-columns: minmax(0, 1fr) auto; }
    .mrow__main { grid-template-columns: minmax(0, 1fr) auto; }
    .mrow__go-text { display: none; }
  }
</style>
