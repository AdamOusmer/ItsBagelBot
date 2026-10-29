<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { onNavigate } from '$app/navigation';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let {
    eyebrow,
    name,
    em = '',
    closeHref,
    closeLabel,
    progress,
    progressLabel,
    count,
    leaving = false,
    status,
    side,
    children
  }: {
    eyebrow: string;
    name: string;
    em?: string;
    closeHref: string;
    closeLabel: string;
    progress: number;
    progressLabel: string;
    count: string;
    leaving?: boolean;
    status?: Snippet;
    side: Snippet;
    children: Snippet;
  } = $props();

  const { t, locale } = getI18n();

  const clamped = $derived(Math.min(Math.max(progress, 0), 1));
  const percent = $derived(new Intl.NumberFormat(locale, { style: 'percent' }).format(clamped));

  onNavigate((navigation) => {
    if (!document.startViewTransition || prefersReducedMotion()) return;
    return new Promise((resolve) => {
      const transition = document.startViewTransition(async () => {
        resolve();
        await navigation.complete;
      });
      transition.ready.catch(() => {});
    });
  });
</script>

<SetupStage label={`${eyebrow} · ${name}`} full fill {leaving} data-orbs="off" data-surface="tonal">
  {#snippet start()}
    <Brand title="ItsBagelBot" sub={t('admin.title')} href="/" logoSrc="/logo.png" logoAlt="" size="md" />
    <div class="ident">
      <Eyebrow tone="go">{eyebrow}</Eyebrow>
      <Heading level={5} as="span">{name} {#if em}<em class="em">{em}</em>{/if}</Heading>
    </div>
  {/snippet}
  {#snippet trail()}
    {@render status?.()}
    <ButtonLink href={closeHref} variant="secondary" size="sm">{closeLabel}</ButtonLink>
  {/snippet}

  <div class="layout">
    <div class="body">
      <aside class="side">
        <Card flush>
          <div class="side-body">{@render side()}</div>
        </Card>
      </aside>
      <section class="theatre">{@render children()}</section>
    </div>
    <div class="meter-row">
      <span class="count">{count}</span>
      <div class="meter">
        <ProgressBar value={clamped} tone="success" size="sm" gradient label={progressLabel} />
      </div>
      <span class="pct">{percent}</span>
    </div>
  </div>
</SetupStage>

<ToastHost />

<style>
  .ident {
    display: grid;
    gap: var(--bb-space-1);
    min-width: 0;
  }
  .em {
    font-style: normal;
    background: var(--bb-gold-gradient);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .layout {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    gap: var(--bb-space-4);
    min-height: 0;
  }
  .body {
    display: grid;
    grid-template-columns: 300px minmax(0, 1fr);
    gap: clamp(20px, 3.5vw, 56px);
    min-height: 0;
  }
  .side {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    align-self: start;
    min-height: 0;
    max-height: 100%;
  }
  .side-body {
    max-height: 100%;
    padding: var(--bb-space-2);
    overflow-y: auto;
    scrollbar-width: thin;
  }
  .theatre {
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-5);
    min-width: 0;
    min-height: 0;
    padding-right: var(--bb-space-2);
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
  }

  .meter-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-4);
  }
  .count,
  .pct {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    letter-spacing: var(--bb-tracking-eyebrow);
    text-transform: uppercase;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .pct {
    min-width: 4ch;
    text-align: right;
    color: var(--bb-tan-pale);
  }
  .meter {
    --progress-from: var(--bb-tan);
    --progress-duration: calc(var(--bb-dur-slow) * 1.5);
  }

  :global(::view-transition-old(root)),
  :global(::view-transition-new(root)) {
    animation-duration: calc(var(--bb-dur-slow) * 1.2);
    animation-timing-function: var(--bb-ease-out-expo);
  }

  @media (max-width: 960px) {
    .body {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto minmax(0, 1fr);
      gap: var(--bb-space-4);
    }
    .side-body {
      max-height: 30svh;
    }
  }
</style>
