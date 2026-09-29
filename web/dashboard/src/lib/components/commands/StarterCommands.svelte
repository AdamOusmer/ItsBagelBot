<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Button, Chip, EmptyState, Label, getI18n, type MessageKey } from '@bagel/kit';
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
  <Button variant="primary" onclick={onNew}>{t('commands.newCommand')}</Button>
</EmptyState>
<div class="starters" role="group" aria-label={t('commands.startersTitle')}>
  <Label mono as="span">{t('commands.startersTitle')}</Label>
  <div class="starters-row">
    {#each starters as s (s.name)}
      <span class="starter"><Chip tone="muted" onclick={() => onPick(s)}>!{s.name}</Chip></span>
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
  .starters-row { display: flex; flex-wrap: wrap; justify-content: center; gap: 8px; }
  .starter { display: inline-flex; }
  @media (pointer: coarse), (max-width: 760px) {
    .starter { min-height: 44px; }
  }
</style>
