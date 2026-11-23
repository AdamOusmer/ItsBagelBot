<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import { ALERT_KINDS, type AlertKind } from '../../lib/overlay-editor/defaults';
  import Fit from './Fit.svelte';
  import Glyph from './Glyph.svelte';

  let { canUndo, canRedo, grid = $bindable(true), panelOpen, onemulate, onundo, onredo, onzoom, onview, onpanel }: {
    canUndo: boolean;
    canRedo: boolean;
    grid?: boolean;
    panelOpen: boolean;
    onemulate: (kind: AlertKind) => void;
    onundo: () => void;
    onredo: () => void;
    onzoom: (direction: -1 | 1) => void;
    onview: (zoom: number) => void;
    onpanel: () => void;
  } = $props();

  const { t } = getI18n();
  let emulateOpen = $state(false);
  let emulateAnchor = $state<HTMLElement>();

  function emulate(kind: AlertKind): void {
    emulateOpen = false;
    onemulate(kind);
  }
</script>

<div class="toolbar" role="toolbar" aria-label={t('overlayEditor.ui.toolbar')}>
  <div class="group panel-toggle">
    <IconButton label={t('overlayEditor.ui.sidePanel')} tooltip aria-pressed={panelOpen} onclick={onpanel}><Icon name="sidebar" size={18} /></IconButton>
  </div>
  <div class="group">
    <span class="anchor" bind:this={emulateAnchor}>
      <Button variant="ghost" class="emulate" aria-haspopup="dialog" aria-expanded={emulateOpen} onclick={() => (emulateOpen = !emulateOpen)}>
        <span class="emulate-body">
          <Icon name="bell" size={18} />
          <span class="emulate-label"><Fit>{t('overlayEditor.ui.emulate')}</Fit></span>
        </span>
      </Button>
    </span>
  </div>
  <div class="group">
    <IconButton label={t('overlayEditor.ui.undo')} tooltip disabled={!canUndo} onclick={onundo}><Glyph name="undo" size={18} /></IconButton>
    <IconButton label={t('overlayEditor.ui.redo')} tooltip disabled={!canRedo} onclick={onredo}><Glyph name="redo" size={18} /></IconButton>
  </div>
  <div class="group zoom">
    <IconButton label={t('overlayEditor.ui.zoomOut')} tooltip onclick={() => onzoom(-1)}><Glyph name="zoomOut" size={18} /></IconButton>
    <IconButton label={t('overlayEditor.ui.zoomIn')} tooltip onclick={() => onzoom(1)}><Glyph name="zoomIn" size={18} /></IconButton>
  </div>
  <div class="group">
    <IconButton label={t('overlayEditor.ui.fit')} tooltip onclick={() => onview(0)}><Glyph name="fit" size={18} /></IconButton>
    <IconButton label={t('overlayEditor.ui.actualSize')} tooltip onclick={() => onview(1)}><Glyph name="actual" size={18} /></IconButton>
  </div>
  <div class="group">
    <IconButton label={t('overlayEditor.ui.grid')} tooltip aria-pressed={grid} onclick={() => (grid = !grid)}><Glyph name="dots" size={18} /></IconButton>
  </div>
</div>

<PickerPanel bind:open={emulateOpen} anchor={emulateAnchor} label={t('overlayEditor.ui.emulate')} placement="below" maxHeight={420}>
  <ul class="options">
    {#each ALERT_KINDS as kind (kind)}
      <PickerOption as="li" label={t(`overlayEditor.alert.${kind}`)} onclick={() => emulate(kind)} />
    {/each}
  </ul>
</PickerPanel>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    max-width: 100%;
    padding: 6px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
  }
  .group { display: flex; align-items: center; gap: 2px; }
  .group + .group { padding-inline-start: 4px; border-inline-start: 1px solid rgba(var(--bb-white-rgb), 0.1); }
  .anchor { display: block; }
  .toolbar :global(.emulate) { width: 68px; height: 44px; --btn-pad: 0; }
  .emulate-body { display: flex; flex-direction: column; align-items: center; gap: 2px; width: 100%; }
  .emulate-label { display: block; width: 100%; height: 14px; font-size: 11px; white-space: nowrap; }
  .options { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  @media (min-width: 1280px) {
    .panel-toggle { display: none; }
    .panel-toggle + .group { padding-inline-start: 0; border-inline-start: 0; }
  }
  @media (max-width: 600px) {
    .zoom { display: none; }
  }
</style>
