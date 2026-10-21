<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, tDiscordModuleLabel, tDiscordModuleTagline, type DiscordModuleDef } from '@bagel/kit';
  import { moduleHrefOf, type ModuleState } from '$lib/discord/module-view';

  let {
    def,
    guildId,
    status
  }: { def: DiscordModuleDef; guildId: string; status: ModuleState } = $props();

  const { t } = getI18n();
  const label = $derived(tDiscordModuleLabel(t, def));
</script>

<ManagementRow as="article" href={moduleHrefOf(guildId, def)} label="{t('discord.overview.configure')}: {label}">
  {#snippet primary()}
    <span class="main">
      <span class="copy">
        <Heading level={6} as="span">{label}</Heading>
        <span class="tagline"><Text as="span" size="xs" tone="muted">{tDiscordModuleTagline(t, def)}</Text></span>
      </span>
      <span class="open-action" aria-hidden="true" title={t('discord.overview.configure')}>
        <Icon name="gear" size={16} />
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="side">
      {#if status === 'core'}
        <Tag tone="pre" data-fit-box><span data-fit="">{t('discord.module.alwaysOn')}</span></Tag>
      {:else if status === 'on'}
        <Tag tone="live" mark="solid">{t('discord.module.statusOn')}</Tag>
      {:else}
        <Tag tone="quiet" mark="hollow">{t('discord.module.statusOff')}</Tag>
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
  .copy {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }
  .tagline {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .side {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    width: 7.25rem;
  }
  @media (max-width: 760px) {
    .main {
      gap: 10px;
    }
    .side {
      width: 6.75rem;
    }
  }
</style>
