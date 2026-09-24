<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { STAGES_FOR } from '$lib/deploys/types';
  import { KIND_KEY, STAGE_KEY, shortSha } from './view';
  import { countKey, shipVersion, type ShipState } from './ship';

  let {
    ship,
    pickedCount,
    services,
    seen
  }: {
    ship: ShipState;
    pickedCount: number;
    services: string[];
    seen: string[];
  } = $props();

  const { t } = getI18n();

  const stages = $derived(STAGES_FOR[ship.kind]);

  function servicesNote(): string {
    if (ship.replanning) return t('admin.deploys.servicesLoading');
    return services.length === 0 ? t('admin.deploys.servicesNone') : '';
  }

  function prsValue(): string {
    if (pickedCount === 0) return t('admin.deploys.flow.reviewPrsNone');
    return t(countKey(pickedCount, 'admin.deploys.flow.picked'), { n: String(pickedCount) });
  }
</script>

<div class="review">
  <dl class="facts">
    <div class="fact">
      <dt>{t('admin.deploys.flow.reviewKind')}</dt>
      <dd>{t(KIND_KEY[ship.kind])}</dd>
    </div>
    {#if ship.uses.version || ship.uses.rollback}
      <div class="fact">
        <dt>{ship.uses.rollback ? t('admin.deploys.rollbackTo') : t('admin.deploys.version')}</dt>
        <dd class="mono">{shipVersion(ship) || '-'}</dd>
      </div>
    {/if}
    {#if ship.uses.target}
      <div class="fact">
        <dt>{t('admin.deploys.target')}</dt>
        <dd class="mono">{shortSha(ship.targetSha) || '-'}</dd>
      </div>
    {/if}
    {#if ship.uses.prs}
      <div class="fact">
        <dt>{t('admin.deploys.flow.reviewPrs')}</dt>
        <dd>{prsValue()}</dd>
      </div>
    {/if}
    {#if ship.uses.changelog}
      <div class="fact wide">
        <dt>{t('admin.deploys.flow.reviewChangelog')}</dt>
        <dd>{ship.titleEn.trim() || '-'}</dd>
      </div>
    {/if}
  </dl>

  <section class="block" aria-labelledby="ship-review-stages">
    <h3 class="sub" id="ship-review-stages">{t('admin.deploys.stages')}</h3>
    <ol class="pipeline">
      {#each stages as s, i (s)}
        <li class="stage">
          <span class="n" aria-hidden="true">{String(i + 1).padStart(2, '0')}</span>
          <span class="label">{t(STAGE_KEY[s])}</span>
        </li>
      {/each}
    </ol>
  </section>

  <section class="block" aria-labelledby="ship-review-services">
    <h3 class="sub" id="ship-review-services">
      {t('admin.deploys.services')}
      <span class="aside">{servicesNote()}</span>
    </h3>
    <div class="chips">
      {#each seen as s (s)}
        {@const rolls = services.includes(s)}
        <span class="chip" class:off={!rolls} aria-hidden={!rolls}>
          <Tag tone={rolls ? 'live' : 'quiet'} mark={rolls ? 'solid' : 'hollow'}>{s}</Tag>
        </span>
      {/each}
    </div>
  </section>
</div>

<style>
  .review {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .facts {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 1px;
    margin: 0;
    overflow: hidden;
    background: var(--bb-border);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
  }
  .fact {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    background: var(--bb-card-bg);
  }
  .fact.wide {
    grid-column: 1 / -1;
  }
  dt {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 700 15px/1.3 var(--bb-font-display);
    color: var(--bb-white);
  }
  dd.mono {
    font-family: var(--bb-font-mono);
    font-weight: 500;
    font-size: 14px;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .sub {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    font-weight: 500;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .aside {
    text-transform: none;
    letter-spacing: 0;
  }
  .pipeline {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .stage {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    height: 28px;
    padding: 0 10px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-pill);
    font-size: 12.5px;
  }
  .n {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-tan);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    min-height: 28px;
  }
  .chip.off {
    opacity: 0.45;
  }
  @media (max-width: 560px) {
    .facts {
      grid-template-columns: 1fr;
    }
  }
</style>
