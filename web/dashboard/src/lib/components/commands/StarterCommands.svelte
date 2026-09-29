<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Chip, EmptyState, getI18n, type MessageKey } from '@bagel/kit';
  import { STARTER_IDS, type Starter, type StarterId } from './starters';

  const { t } = getI18n();

  let { onNew, onPick }: { onNew: () => void; onPick: (starter: Starter) => void } = $props();

  const starter = (id: StarterId): Starter => ({
    name: t(`commands.starters.${id}.name` as MessageKey),
    response: t(`commands.starters.${id}.response` as MessageKey)
  });
  const starters = $derived(STARTER_IDS.map(starter));
</script>

<EmptyState title={t('commands.noneYet')} body={t('commands.noneYetBody', { name: 'name' })}>
  <button class="bb-btn bb-btn--primary" type="button" onclick={onNew}>{t('commands.newCommand')}</button>
</EmptyState>
<div class="starters" role="group" aria-label={t('commands.startersTitle')}>
  <span class="starters-title">{t('commands.startersTitle')}</span>
  <div class="starters-row">
    {#each starters as s (s.name)}
      <Chip tone="muted" class="starter" onclick={() => onPick(s)}>!{s.name}</Chip>
    {/each}
  </div>
</div>

<style>
  .starters {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 0 16px 24px;
  }
  .starters-title {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .starters-row { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
  @media (pointer: coarse), (max-width: 760px) {
    .starters-row :global(.starter) { min-height: 44px; }
  }
</style>
