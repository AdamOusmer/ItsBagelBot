<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { STAGES_FOR } from '#lib/deploys/types';
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

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);

  const stages = $derived(STAGES_FOR[ship.kind]);

  function servicesNote(): string {
    if (ship.replanning) return t('admin.deploys.servicesLoading');
    return services.length === 0 ? t('admin.deploys.servicesNone') : '';
  }

  function prsValue(): string {
    if (pickedCount === 0) return t('admin.deploys.flow.reviewPrsNone');
    return t(countKey(pickedCount, 'admin.deploys.flow.picked'), { n: number.format(pickedCount) });
  }
</script>

<Stack gap={3}>
  <FactList layout="tiles" style="grid-template-columns: repeat(2, minmax(0, 1fr));">
    <Fact fit term={t('admin.deploys.flow.reviewKind')}>{t(KIND_KEY[ship.kind])}</Fact>
    {#if ship.uses.version || ship.uses.rollback}
      <Fact fit term={ship.uses.rollback ? t('admin.deploys.rollbackTo') : t('admin.deploys.version')} mono>{shipVersion(ship) || '-'}</Fact>
    {/if}
    {#if ship.uses.target}
      <Fact fit term={t('admin.deploys.target')} mono>{shortSha(ship.targetSha) || '-'}</Fact>
    {/if}
    {#if ship.uses.prs}
      <Fact fit term={t('admin.deploys.flow.reviewPrs')}>{prsValue()}</Fact>
    {/if}
    {#if ship.uses.changelog}
      <Fact fit term={t('admin.deploys.flow.reviewChangelog')} wide>{ship.titleEn.trim() || '-'}</Fact>
    {/if}
  </FactList>

  <Stack as="section" gap={2} aria-labelledby="ship-review-stages">
    <Heading variant="label" id="ship-review-stages">{t('admin.deploys.stages')}</Heading>
    <ol class="pipeline" role="list">
      {#each stages as stage (stage)}
        <li>
          <Badge shape="pill" tone="neutral"><bb-fit><span data-fit>{t(STAGE_KEY[stage])}</span></bb-fit></Badge>
        </li>
      {/each}
    </ol>
  </Stack>

  <Stack as="section" gap={2} aria-labelledby="ship-review-services">
    <Heading variant="label" id="ship-review-services">
      {t('admin.deploys.services')}
      <span class="aside">{servicesNote()}</span>
    </Heading>
    <div class="chips">
      {#each seen as s (s)}
        {@const rolls = services.includes(s)}
        <span class="service" class:omitted={!rolls} aria-hidden={!rolls}>
          <Chip as="span" pressed={rolls}>{s}</Chip>
        </span>
      {/each}
    </div>
  </Stack>
</Stack>

<style>
  .aside {
    margin-left: var(--bb-space-2);
    text-transform: none;
    letter-spacing: 0;
  }
  .pipeline {
    display: flex;
    flex-wrap: wrap;
    gap: var(--bb-space-1) var(--bb-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
    line-height: 1.3;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--bb-space-2);
    min-height: 28px;
  }

  .service {
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .service.omitted {
    opacity: 0.45;
  }

  @media (prefers-reduced-motion: reduce) {
    .service {
      transition: none;
    }
  }
</style>
