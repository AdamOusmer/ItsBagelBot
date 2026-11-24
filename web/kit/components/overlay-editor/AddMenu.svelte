<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { EditableKind } from '../../lib/overlay-editor/defaults';
  import { filterKinds, groupIcon, KIND_GROUPS, type KindGroupKey, type KindLabel } from '../../lib/overlay-editor/kinds';
  import Fit from './Fit.svelte';
  import Reveal from './Reveal.svelte';

  let { open = $bindable(false), anchor, full, onpick }: {
    open?: boolean;
    anchor?: HTMLElement;
    full: boolean;
    onpick: (kind: EditableKind) => void;
  } = $props();

  const { t } = getI18n();
  let query = $state('');
  let category = $state<KindGroupKey | ''>('');

  const groups = $derived(
    KIND_GROUPS.map((group) => ({
      key: group.key,
      name: t(`overlayEditor.group.${group.key}`),
      items: group.kinds.map((kind) => ({ kind, name: t(`overlayEditor.kind.${kind}`), hint: t(`overlayEditor.kindHint.${kind}`) }))
    }))
  );
  const found = $derived(filterKinds(groups.flatMap((group) => group.items), query));
  const searching = $derived(query.trim() !== '');

  $effect(() => {
    if (open) return;
    query = '';
    category = '';
  });

  const expand = (key: KindGroupKey): void => {
    category = category === key ? '' : key;
  };
</script>

{#snippet entry(item: KindLabel)}
  <PickerOption as="li" layout="stacked" label={item.name} description={item.hint} disabled={full} onclick={() => onpick(item.kind)} />
{/snippet}

<PickerPanel bind:open {anchor} label={t('overlayEditor.ui.addWidget')} placement="beside" width={340} maxHeight={520}>
  <SearchInput fill bind:value={query} placeholder={t('overlayEditor.ui.searchWidgets')} aria-label={t('overlayEditor.ui.searchWidgets')} />
  {#if searching}
    <ul class="items">
      {#each found as item (item.kind)}
        {@render entry(item)}
      {:else}
        <li class="empty">{t('overlayEditor.ui.noWidgetResults')}</li>
      {/each}
    </ul>
  {:else}
    {#each groups as group (group.key)}
      <div class="group">
        <PickerOption aria-expanded={category === group.key} onclick={() => expand(group.key)}>
          <IconTile size="sm" style="--tile: 28px"><Icon name={groupIcon(group.key)} size={14} /></IconTile>
          <span class="card-name"><Fit>{group.name}</Fit></span>
          {#snippet trailing()}
            <Icon name="chevron" size={14} class={category === group.key ? 'chev open' : 'chev'} />
          {/snippet}
        </PickerOption>
        <Reveal open={category === group.key}>
          <ul class="items">
            {#each group.items as item (item.kind)}
              {@render entry(item)}
            {/each}
          </ul>
        </Reveal>
      </div>
    {/each}
  {/if}
</PickerPanel>

<style>
  .group { display: flex; flex-direction: column; }
  .card-name { flex: 1; min-width: 0; font-size: 13.5px; font-weight: 600; color: var(--bb-white); }
  .group :global(.chev) { transition: transform var(--bb-dur-fast) var(--bb-ease-out-expo); }
  .group :global(.chev.open) { transform: rotate(180deg); }
  .items { list-style: none; margin: 0; padding: 2px 0 6px 12px; display: flex; flex-direction: column; gap: 2px; }
  .empty { padding: 12px 6px; font-size: 13px; color: var(--bb-muted); }
</style>
