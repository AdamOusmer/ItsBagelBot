<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatePill from '#lib/components/StatePill.svelte';
  import type { DeployStage } from '#lib/deploys/types';
  import NowPanel from './NowPanel.svelte';
  import StageDetail from './StageDetail.svelte';
  import { STAGE_ABOUT_KEY, STAGE_KEY, STAGE_STATE_BODY_KEY, STEP_STATE_KEY, stageMeta, stageTone, stageValue } from './view';

  let { stage, count, heading = $bindable(null) }: {
    stage: DeployStage;
    count: string;
    heading?: HTMLElement | null;
  } = $props();

  const { t } = getI18n();
  const title = $derived(t(STAGE_KEY[stage.id]));
  const meta = $derived(stageMeta(stage, t));
  const value = $derived(stageValue(stage));
  const showBuild = $derived(stage.id === 'build' && (stage.items?.length ?? 0) > 0);
  const hasDetail = $derived(Boolean(stage.items?.length || stage.links?.length || stage.failure?.log_tail?.length));
</script>

<Stack gap={4} class="stage-panel">
  <Card as="section" aria-labelledby="run-stage-title">
    <div class="summary">
      <Text size="xs" tone="muted" mono>
        <bb-fit><span data-fit>{count}</span></bb-fit>
      </Text>
      <div class="stage-title bb-fit-box">
        <Heading level={2} variant="card" id="run-stage-title" tabindex={-1} bind:element={heading}>
          <bb-fit><span data-fit>{title}</span></bb-fit>
        </Heading>
      </div>
      <div class="description bb-fit-box">
        <Text size="sm" tone="muted">
          <bb-fit mode="block"><span data-fit>{t(STAGE_ABOUT_KEY[stage.id])}</span></bb-fit>
        </Text>
      </div>
      <div class="stage-status">
        <div class="stage-state bb-fit-box">
          <StatePill tone={stageTone(stage.state)}>
            <bb-fit><span data-fit>{t(STEP_STATE_KEY[stage.state])}</span></bb-fit>
          </StatePill>
        </div>
        <div class="state-body bb-fit-box">
          <Text as="span" size="sm">
            <bb-fit mode="block"><span data-fit>{t(STAGE_STATE_BODY_KEY[stage.state])}</span></bb-fit>
          </Text>
        </div>
      </div>
      <div class="progress" class:unavailable={value === undefined} aria-hidden={value === undefined}>
        <ProgressBar value={value === undefined ? 0 : value} tone={stageTone(stage.state)} label={title} size="sm" />
      </div>
      <div class="meta bb-fit-box" class:unavailable={!meta} aria-hidden={!meta}>
        <Text size="xs" tone="muted" mono>
          <bb-fit mode="block"><span data-fit>{meta}</span></bb-fit>
        </Text>
      </div>
    </div>
  </Card>

  {#if showBuild}
    <NowPanel {stage} />
  {/if}
  {#if hasDetail}
    <StageDetail {stage} />
  {/if}
</Stack>

<style>
  .summary {
    display: grid;
    gap: var(--bb-space-3);
    min-width: 0;
  }
  .description,
  .stage-title {
    min-inline-size: 0;
  }
  .description {
    block-size: 3.375rem;
  }
  .stage-status {
    display: grid;
    grid-template-columns: minmax(0, 7.5rem) minmax(0, 1fr);
    align-items: center;
    justify-items: start;
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .state-body {
    display: grid;
    align-items: center;
    width: 100%;
    block-size: 2rem;
  }
  .stage-state {
    min-inline-size: 0;
    max-inline-size: 100%;
  }
  .meta {
    block-size: 2.5rem;
    overflow: auto;
    overflow-wrap: anywhere;
  }
  .meta,
  .progress {
    transition: opacity var(--bb-dur-base) ease;
  }
  .unavailable {
    opacity: 0;
  }
  @media (max-width: 420px) {
    .stage-status {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .meta,
    .progress {
      transition: none;
    }
  }
</style>
