<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/row-body.css';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, tDiscordModuleLabel, tDiscordModuleTagline, type DiscordModuleDef } from '@bagel/kit';
  import { moduleHrefOf, type ModuleState } from '#lib/discord/module-view';

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
    <span class="bb-row-body--main">
      <span class="bb-row-body__copy">
        <Heading level={6} as="span">{label}</Heading>
        <span class="bb-row-body__tagline"><Text as="span" size="xs" tone="muted">{tDiscordModuleTagline(t, def)}</Text></span>
      </span>
      <span class="bb-row-body__go" aria-hidden="true" title={t('discord.overview.configure')}>
        <Icon name="gear" size={16} />
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="bb-row-body__side">
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
