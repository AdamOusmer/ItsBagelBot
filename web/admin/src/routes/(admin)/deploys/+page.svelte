<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusStrip from '$lib/components/deploys/StatusStrip.svelte';
  import RunHistory from '$lib/components/deploys/RunHistory.svelte';

  let { data } = $props();

  const { t } = getI18n();

  const last = $derived(data.runs.find((r) => r.id !== data.active?.id) ?? null);
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.deploys.eyebrow')} description={t('admin.deploys.description')}>
    {t('admin.deploys.titlePre')}<em>{t('admin.deploys.titleEm')}</em>
  </PageHead>

  {#if data.runsError}
    <AlertBanner>{t('admin.deploys.planError', { error: data.runsError })}</AlertBanner>
  {/if}

  {#await data.planned}
    <SkeletonStack rows={1} height="124px" />
  {:then planned}
    <StatusStrip plan={planned.plan} active={data.active} {last} />
    {#if planned.planError}
      <AlertBanner>{t('admin.deploys.planError', { error: planned.planError })}</AlertBanner>
    {/if}
  {/await}

  <div class="stack">
    <div class="start">
      <div class="copy">
        <h2>{t('admin.deploys.start.title')}</h2>
        <p>{data.active ? t('admin.deploys.runActive') : t('admin.deploys.start.body')}</p>
      </div>
      {#if data.active}
        <ButtonLink href="/deploys/{data.active.id}" variant="green" solid>{t('admin.deploys.openRun')}</ButtonLink>
      {:else}
        <ButtonLink href="/deploys/new" variant="green" solid>{t('admin.deploys.start.cta')}</ButtonLink>
      {/if}
    </div>
    <RunHistory runs={data.runs} />
  </div>
</section>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-top: 16px;
  }
  .start {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px 24px;
    flex-wrap: wrap;
    padding: clamp(18px, 3vw, 28px);
    background:
      radial-gradient(120% 90% at 0% 0%, rgba(var(--bb-green-glow-rgb), 0.08), transparent 60%),
      radial-gradient(90% 80% at 100% 100%, rgba(var(--bb-tan-rgb), 0.07), transparent 60%),
      var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-lg);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.06);
  }
  .copy {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  h2 {
    margin: 0;
    font: 700 clamp(1.3rem, 2.2vw, 1.7rem) / 1.1 var(--bb-font-display);
    letter-spacing: -0.02em;
    color: var(--bb-white);
  }
  p {
    margin: 0;
    max-width: 56ch;
    font-size: 14px;
    color: var(--bb-muted);
  }
</style>
