<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type GoveeDevice, type GoveeBinding } from '@bagel/kit';
  import RowDeleteButton from '$lib/components/shared/RowDeleteButton.svelte';

  const { t } = getI18n();

  let {
    device,
    binding,
    expanded = false,
    onExpand,
    onDelete
  }: {
    device: GoveeDevice;
    binding: GoveeBinding | null;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
  } = $props();

  const reward = $derived(binding?.reward ?? null);
  const lightName = $derived(device.name || device.device);
</script>

<ManagementRow
  selected={expanded}
  {expanded}
  controls="govee-editor"
  onSelect={onExpand}
>
  {#snippet primary()}
    <span class="prow">
      <span class="light">
        <span class="swatch" style="--sw: {reward?.color || 'var(--bb-muted)'}" aria-hidden="true"></span>
        <span class="light-text">
          <span class="light-name" class:unset={!binding}>{lightName}</span>
          <Text as="span" size="xs" mono tone="muted">{device.sku}</Text>
        </span>
      </span>
      <span class="status">
        {#if reward}
          <Text as="span" size="sm" truncate>{reward.title}</Text>
          <Text as="span" size="xs" mono tone="accent">{t('govee.costPts', { n: reward.cost.toLocaleString() })}</Text>
        {:else}
          <span class="unset-tag"><Tag tone="quiet" mark="hollow">{t('govee.notSetUp')}</Tag></span>
        {/if}
      </span>
      <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="chevron" size={13} /></span>
    </span>
  {/snippet}
  {#snippet actions()}
    {#if binding}
      <RowDeleteButton label={t('govee.removeAria', { name: lightName })} onclick={onDelete} />
    {/if}
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: minmax(150px, 1.2fr) minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }

  .light { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: none;
    border-radius: var(--bb-radius-sm);
    background: var(--sw);
    border: 1px solid color-mix(in srgb, var(--sw) 55%, transparent);
  }
  .light-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .light-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  .light-name.unset { color: var(--bb-muted); }

  .status { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .unset-tag { align-self: flex-start; }

  .chev {
    display: inline-flex;
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease, transform var(--bb-dur-fast) ease;
  }
  .chev.open { color: var(--bb-tan); transform: rotate(180deg); }

  @media (max-width: 620px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light chev' 'status chev'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .chev { grid-area: chev; }
  }
</style>
