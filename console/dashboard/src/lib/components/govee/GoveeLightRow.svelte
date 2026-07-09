<script lang="ts">
  // One light in the govee deck: the device on the left, its bound reward (or an
  // empty "not set up" state) on the right. Selecting it loads the reward into
  // the page's inspector. One reward per light, so a row has at most one reward.
  import { Icon, type GoveeDevice, type GoveeBinding } from '@bagel/shared';

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

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div class="row-shell {expanded ? 'selected' : ''} {binding ? '' : 'unset'}">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="trow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
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

    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      {#if binding}
        <button class="mini" type="button" aria-label="Remove the reward for {device.name || device.device}" onclick={onDelete}>
          <Icon name="trash" size={15} />
        </button>
      {/if}
      <span class="chev" class:open={expanded} aria-hidden="true"><Icon name="settings" size={13} /></span>
    </span>
  </div>
</div>

<style>
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }
  .row-shell.unset .light-name { color: var(--bb-muted); }

  .trow {
    display: grid;
    grid-template-columns: minmax(150px, 1.2fr) minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    padding: 12px 14px;
    cursor: pointer;
    user-select: none;
  }
  .trow:hover { background: rgba(201, 168, 124, 0.045); }
  .trow:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

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

  .row-act { display: inline-flex; align-items: center; gap: 8px; }
  .chev { display: inline-flex; color: var(--bb-muted); transition: color var(--bb-dur-fast, 140ms) ease; }
  .row-shell.selected .chev, .chev.open { color: var(--bb-tan); }

  @media (max-width: 620px) {
    .trow { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'light act' 'status act'; row-gap: 4px; }
    .light { grid-area: light; }
    .status { grid-area: status; }
    .row-act { grid-area: act; }
  }
</style>
