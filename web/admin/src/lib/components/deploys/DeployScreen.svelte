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

<SetupStage label={`${eyebrow} · ${name}`} full fill {leaving} orbs={false}>
  {#snippet start()}
    <div class="brand"><Brand title="ItsBagelBot" sub={t('admin.title')} href="/" logoSrc="/logo.png" size="sm" /></div>
    <div class="ident bb-fit-box">
      <Eyebrow tone="go"><bb-fit><span data-fit>{eyebrow}</span></bb-fit></Eyebrow>
      <Heading level={5} as="span">
        <bb-fit><span data-fit>{name} {#if em}<em class="em">{em}</em>{/if}</span></bb-fit>
      </Heading>
    </div>
  {/snippet}
  {#snippet trail()}
    <ButtonLink href={closeHref} variant="secondary" size="sm">
      <span class="close-copy"><bb-fit><span data-fit>{closeLabel}</span></bb-fit></span>
    </ButtonLink>
  {/snippet}

  <div class="layout">
    <div class="body">
      <aside class="side">
        <Card flush>
          <div class="side-body">{@render side()}</div>
        </Card>
      </aside>
      <main class="theatre">{@render children()}</main>
    </div>
    <div class="meter-row">
      <span class="count"><bb-fit><span data-fit>{count}</span></bb-fit></span>
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
    inline-size: min(18rem, 50vw);
  }

  .brand {
    flex: none;
  }

  .close-copy {
    display: block;
    inline-size: 7rem;
  }

  .em {
    font-style: normal;
    color: var(--bb-tan-pale);
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
    overflow: auto;
    scrollbar-width: thin;
  }
  .theatre {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding: var(--bb-space-1);
  }

  .meter-row {
    display: grid;
    grid-template-columns: 13ch minmax(0, 1fr) 4ch;
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
  }

  @media (max-width: 31rem) {
    .brand {
      display: none;
    }

    .meter-row {
      gap: var(--bb-space-2);
    }

    .ident {
      inline-size: min(16rem, 45vw);
    }

    .close-copy {
      inline-size: 6rem;
    }
  }
</style>
