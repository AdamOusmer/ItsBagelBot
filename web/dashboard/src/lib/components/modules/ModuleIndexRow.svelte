<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import '@bagel/ui/styles/elements/row-body.css';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
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

<ManagementRow as="article" {href} label="{t('modules.openSettings')}: {tModuleLabel(t, def)}">
  {#snippet primary()}
    <span class="bb-row-body--main">
      <span class="bb-row-body__copy">
        <Heading level={6} as="span">
          {tModuleLabel(t, def)}
          {#if beta}<span class="beta"><Tag tone="alpha">{t('modules.betaChip')}</Tag></span>{/if}
        </Heading>
        <span class="bb-row-body__tagline"><Text as="span" size="xs" tone="muted">{tModuleTagline(t, def)}</Text></span>
        {#if chips.chips.length}
          <span class="bb-row-body__cmds">
            {#each chips.chips as chip (chip)}
              <Tag bare literal>{chip}</Tag>
            {/each}
            {#if chips.extra}
              <Tag bare literal>{t('modules.moreCommands', { n: chips.extra })}</Tag>
            {/if}
          </span>
        {/if}
      </span>
      <span class="bb-row-body__go" aria-hidden="true" title={t('modules.openSettings')}>
        <Icon name="gear" size={16} />
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="bb-row-body__side">
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
            busy={status === 'saving'}
          />
        </form>
      {:else}
        <Tag tone="pre" data-fit-box><span data-fit="">{t('modules.alwaysOn')}</span></Tag>
      {/if}
    </span>
  {/snippet}
</ManagementRow>

<style>
  .beta {
    display: inline-flex;
    vertical-align: 1px;
    margin-left: 8px;
  }
</style>
