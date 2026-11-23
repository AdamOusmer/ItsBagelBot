<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { mediaQuery } from '@bagel/ui/lib/motion-query';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { STEP_BODY_KEY, STEP_TITLE_KEY, type ShipStep } from './ship';

  let {
    current,
    feedback,
    issue,
    pending,
    leaving,
    actions,
    children
  }: {
    current: ShipStep;
    feedback: string;
    issue: string;
    pending: boolean;
    leaving: boolean;
    actions: Snippet;
    children: Snippet;
  } = $props();

  const { t } = getI18n();
  const face: Record<ShipStep, string> = {
    kind: 'curious',
    build: 'attentive',
    rollback: 'attentive',
    prs: 'curious',
    changelog: 'happy',
    review: 'excited'
  };

  let viewport = $state<HTMLDivElement | null>(null);
  let shown: ShipStep | undefined;
  let showMascot = $state(false);
  const title = $derived(t(STEP_TITLE_KEY[current]));

  $effect(() => {
    const query = mediaQuery('(min-width: 70rem)');
    const sync = () => (showMascot = query.matches);
    sync();
    query.addEventListener('change', sync);
    return () => query.removeEventListener('change', sync);
  });

  $effect(() => {
    const next = current;
    if (!viewport || shown === next) return;
    if (shown) {
      viewport.scrollTop = 0;
      viewport.querySelector('h1')?.focus({ preventScroll: true });
    }
    shown = next;
  });
</script>

<div class="stage">
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="viewport" bind:this={viewport} role="region" aria-label={title} tabindex={0}>
    <PageHead compact>
      <bb-fit><span data-fit>{title}</span></bb-fit>
    </PageHead>
    <div class="description">
      <Text as="div" size="sm" tone="muted" style="block-size: 100%;"><bb-fit mode="block"><span data-fit>{t(STEP_BODY_KEY[current])}</span></bb-fit></Text>
    </div>

    <div class="step-body" class:with-mascot={current !== 'kind'}>
      <div class="fields" aria-busy={pending}>{@render children()}</div>
      {#if showMascot && current !== 'kind'}
        <div class="mascot">
          {#await import('./Mascot.svelte') then { default: Mascot }}
            {#key current}
              <Mascot expression={leaving ? 'proud' : face[current]} sequence={leaving ? 'burst' : 'entrance'} sequenceKey={Number(leaving)} />
            {/key}
          {/await}
        </div>
      {/if}
    </div>
  </div>

  <footer class="footer">
    <Cluster justify="between" gap={3}>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="feedback" class:with-issue={Boolean(issue)} role={issue ? 'region' : undefined} aria-label={issue || undefined} tabindex={issue ? 0 : undefined}>
        {#if issue}
          <AlertBanner tone="danger" flush>{issue}</AlertBanner>
        {:else}
          <div class="status" role="status">
            {#if pending}
              <Skeleton width="min(100%, 10rem)" />
              <VisuallyHidden>{feedback}</VisuallyHidden>
            {:else}
              <Text as="div" size="sm" tone="pale"><bb-fit mode="block"><span data-fit>{feedback}</span></bb-fit></Text>
            {/if}
          </div>
        {/if}
      </div>
      <div class="buttons">{@render actions()}</div>
    </Cluster>
  </footer>
</div>

<style>
  .stage {
    display: grid;
    flex: 1;
    grid-template-rows: minmax(0, 1fr) auto;
    min-block-size: 0;
    min-inline-size: 0;
  }

  .viewport {
    min-block-size: 0;
    min-inline-size: 0;
    overflow: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    padding-inline-end: var(--bb-space-1);
  }

  .viewport:focus-visible {
    outline: 2px solid var(--bb-focus);
    outline-offset: -2px;
  }

  .description {
    block-size: 3.5rem;
    max-inline-size: 45rem;
  }

  .step-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-inline-size: 0;
    gap: var(--bb-space-3);
    padding-block: var(--bb-space-1);
  }

  .fields,
  .feedback {
    min-inline-size: 0;
  }

  .footer {
    display: grid;
    gap: var(--bb-space-2);
    min-inline-size: 0;
    padding-block-start: var(--bb-space-2);
  }

  .feedback {
    flex: 1;
    block-size: 3rem;
    overflow: hidden;
  }

  .status {
    display: grid;
    align-content: center;
    block-size: 100%;
  }

  .feedback.with-issue {
    overflow: auto;
    overflow-wrap: anywhere;
    scrollbar-width: thin;
  }

  .buttons {
    display: flex;
    flex: none;
    gap: var(--bb-space-2);
  }

  .mascot {
    display: none;
  }

  @media (min-width: 70rem) {
    .step-body.with-mascot {
      grid-template-columns: minmax(0, 1fr) 13rem;
    }

    .mascot {
      display: block;
    }
  }

  @media (max-width: 40rem) {
    .feedback {
      flex-basis: 100%;
    }

    .buttons {
      margin-inline-start: auto;
    }
  }
</style>
