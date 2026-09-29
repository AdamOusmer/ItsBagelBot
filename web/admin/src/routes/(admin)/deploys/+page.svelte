<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
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

  <div class="runs">
    <Stack gap={4}>
      <div class="start">
        <Card>
          <Cluster justify="between" gap={4}>
            <div class="copy">
              <Stack gap={1}>
                <Heading level={3} as="h2">{t('admin.deploys.start.title')}</Heading>
                <Text size="sm" tone="muted">{data.active ? t('admin.deploys.runActive') : t('admin.deploys.start.body')}</Text>
              </Stack>
            </div>
            {#if data.active}
              <ButtonLink href="/deploys/{data.active.id}" tone="success">{t('admin.deploys.openRun')}</ButtonLink>
            {:else}
              <ButtonLink href="/deploys/new" tone="success">{t('admin.deploys.start.cta')}</ButtonLink>
            {/if}
          </Cluster>
        </Card>
      </div>
      <RunHistory runs={data.runs} />
    </Stack>
  </div>
</section>

<style>
  .runs {
    margin-top: var(--bb-space-4);
  }
  .start {
    --card-pad: clamp(18px, 3vw, 28px);
    --card-radius: var(--bb-radius-lg);
    --card-bg:
      radial-gradient(120% 90% at 0% 0%, rgba(var(--bb-green-glow-rgb), 0.08), transparent 60%),
      radial-gradient(90% 80% at 100% 100%, rgba(var(--bb-tan-rgb), 0.07), transparent 60%),
      var(--bb-card-bg);
  }
  .copy {
    min-width: 0;
    max-width: 56ch;
  }
</style>
