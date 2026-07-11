<script lang="ts">
  // One light in the govee deck, on the shared ManagementRow: the clickable
  // primary is a real button; the remove-reward action is its sibling. Selecting
  // it loads the reward into the page's inspector. One reward per light.
  import { Icon, ManagementRow, type GoveeDevice, type GoveeBinding } from '@bagel/shared';

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
</script>

<div class="row-wrap" class:unset={!binding}>
  <ManagementRow
    selected={expanded}
    {expanded}
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
        <span class="light">
          <span class="swatch" style="--sw: {reward?.color || '#6b7079'}"><Icon name="power" size={12} /></span>
          <span class="light-text">
            <span class="light-name">{device.name || device.device}</span>
            <span class="light-sku">{device.sku}</span>
          </span>
        </span>
        <span class="status">
          {#if reward}
            <span class="reward-title">{reward.title}</span>
            <span class="reward-cost">{reward.cost.toLocaleString()} pts</span>
          {:else}
            <span class="unset-tag">Not set up</span>
          {/if}
        </span>
        <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="settings" size={13} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      {#if binding}
        <button class="mini" type="button" aria-label="Remove the reward for {device.name || device.device}" onclick={onDelete}>
          <Icon name="trash" size={15} />
        </button>
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

  .mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
  }
  .mini:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }
  .mini:focus-visible { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  @media (max-width: 620px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light chev' 'status chev'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .chev { grid-area: chev; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
