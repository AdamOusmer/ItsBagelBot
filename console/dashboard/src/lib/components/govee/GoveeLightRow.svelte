<script lang="ts">
  // One light in the govee deck, on the shared ManagementRow: the clickable
  // primary is a real button (aria-controls the reward inspector); the
  // remove-reward action is its sibling, never nested inside it. Selecting a row
  // loads its reward into the page's inspector. One reward per light.
  import { Icon, ManagementRow, MiniButton, getI18n, type GoveeDevice, type GoveeBinding } from '@bagel/shared';

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

<div class="row-wrap" class:unset={!binding}>
  <ManagementRow
    selected={expanded}
    {expanded}
    controls="govee-editor"
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        <span class="light">
          <span class="swatch" style="--sw: {reward?.color || '#6b7079'}" aria-hidden="true"><Icon name="power" size={12} /></span>
          <span class="light-text">
            <span class="light-name">{lightName}</span>
            <span class="light-sku">{device.sku}</span>
          </span>
        </span>
        <!-- Reward + unset states are spelled out in TEXT, not by colour alone. -->
        <span class="status">
          {#if reward}
            <span class="reward-title">{reward.title}</span>
            <span class="reward-cost">{t('govee.costPts', { n: reward.cost.toLocaleString() })}</span>
          {:else}
            <span class="unset-tag">{t('govee.notSetUp')}</span>
          {/if}
        </span>
        <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="settings" size={13} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if binding}
        <MiniButton icon="trash" class="row-del" aria-label={t('govee.removeAria', { name: lightName })} onclick={onDelete} />
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  .row-wrap.unset .light-name { color: var(--bb-muted); }

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
    border-radius: 7px;
    background: color-mix(in srgb, var(--sw) 22%, transparent);
    border: 1px solid color-mix(in srgb, var(--sw) 55%, transparent);
    color: color-mix(in srgb, var(--sw) 78%, white);
  }
  .swatch :global(svg) { stroke-width: 1.8; }
  .light-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .light-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .light-sku { font-family: var(--bb-font-mono, monospace); font-size: 11px; color: var(--bb-muted); }

  .status { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .reward-title {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reward-cost { font-family: var(--bb-font-mono, monospace); font-size: 11.5px; color: var(--bb-tan-light); }
  .unset-tag {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    font-style: italic;
  }

  .chev { display: inline-flex; color: var(--bb-muted); transition: color var(--bb-dur-fast, 140ms) ease; }
  .chev.open { color: var(--bb-tan); }

  /* Give the borderless mini delete a >=44px hit target (WCAG 2.2). */
  :global(.mini.row-del) { width: 44px; height: 44px; border-radius: 8px; }
  :global(.mini.row-del:hover) { color: #cf8a78; }
  :global(.mini.row-del:focus-visible) { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  @media (max-width: 620px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light chev' 'status chev'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .chev { grid-area: chev; }
  }
</style>
