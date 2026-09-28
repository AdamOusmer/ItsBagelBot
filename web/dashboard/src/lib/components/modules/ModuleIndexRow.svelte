<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Heading,
    Icon,
    ManagementRow,
    SaveStatus,
    Switch,
    Tag,
    Text,
    TextLink
  } from '@bagel/ui/svelte';
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

<ManagementRow as="article" {href} label="{t('modules.openSettings')}: {tModuleLabel(t, def)}">
  {#snippet primary()}
    <span class="main">
      <span class="copy">
        <Heading level={6} as="span">
          {tModuleLabel(t, def)}
          {#if beta}<span class="beta"><Tag tone="alpha">{t('modules.betaChip')}</Tag></span>{/if}
        </Heading>
        <span class="tagline"><Text as="span" size="xs" tone="muted">{tModuleTagline(t, def)}</Text></span>
        {#if chips.chips.length}
          <span class="cmds">
            {#each chips.chips as chip (chip)}
              <Tag bare literal>{chip}</Tag>
            {/each}
            {#if chips.extra}
              <Tag bare literal>{t('modules.moreCommands', { n: chips.extra })}</Tag>
            {/if}
          </span>
        {/if}
      </span>
      <span class="open-action" aria-hidden="true" title={t('modules.openSettings')}>
        <Icon name="gear" size={16} />
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="side">
      <SaveStatus state={status} compact />
      {#if locked}
        <TextLink href="/billing" label={t('modules.betaPremium')} />
      {:else if toggleable}
        {#if module.enabled}
          <Tag tone="live" mark="solid">{t('modules.statusOn')}</Tag>
        {/if}
        <form method="POST" action="?/toggle" use:enhance={toggleSubmit}>
          <input type="hidden" name="name" value={def.id} />
          <input type="hidden" name="is_enabled" value={module.enabled ? '' : 'on'} />
          <Switch
            type="submit"
            checked={module.enabled}
            label={module.enabled ? t('modules.disableAria', { label: tModuleLabel(t, def) }) : t('modules.enableAria', { label: tModuleLabel(t, def) })}
            pending={status === 'saving'}
          />
        </form>
      {:else}
        <Tag tone="pre">{t('modules.alwaysOn')}</Tag>
      {/if}
    </span>
  {/snippet}
</ManagementRow>

<style>
  .main {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-width: 0;
  }
  .open-action {
    display: inline-flex;
    align-items: center;
    flex: none;
    margin-left: auto;
    padding: 8px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    color: var(--bb-tan-light);
  }
  :global(:hover) > .main .open-action { border-color: var(--bb-tan); }

  .copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .tagline {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .cmds {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 14px;
    margin-top: 6px;
  }

  .beta {
    display: inline-flex;
    vertical-align: 1px;
    margin-left: 8px;
  }
  .side {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    width: 7.25rem;
  }

  @media (max-width: 760px) {
    .main { gap: 10px; }
    .side { width: 6.75rem; }
  }
</style>
