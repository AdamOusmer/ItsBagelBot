<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import type { StatusTone } from '@bagel/kit/status-tone';

  type HistoryRow = {
    id: number | string;
    label: string;
    when: string;
    outcome?: string;
    tone: StatusTone;
  };

  let { title, loading, empty, rows, error, more }: {
    title: string;
    loading: string;
    empty: string;
    rows: HistoryRow[] | null;
    error: string;
    more?: { href: string; label: string };
  } = $props();

  const heading = $props.id();
  const VISIBLE_ROWS = 4;
  const pending = $derived(rows === null);
  const hasRows = $derived(Boolean(rows?.length));
  const hasMoreRows = $derived((rows?.length ?? 0) > 5);
</script>

<Stack as="section" gap={2} aria-labelledby={heading}>
  <Heading id={heading} level={3} variant="label">{title}</Heading>
  <div class="history">
    {#if pending}
      <span class="bb-sr-only" role="status">{loading}</span>
      <SkeletonStack rows={VISIBLE_ROWS} height="1rem" />
    {:else if error}
      <Scroller maxHeight="100%" role="region" aria-labelledby={heading} tabindex={0}>
        <div class="error">
          <AlertBanner tone="danger" flush>{error}</AlertBanner>
        </div>
      </Scroller>
    {:else if !hasRows}
      <EmptyState style="--empty-pad: var(--bb-space-6) 0" title={empty} />
    {:else}
      <Scroller maxHeight="100%" role="region" aria-labelledby={heading} tabindex={hasMoreRows ? 0 : undefined}>
        <ul class="rows">
          {#each rows ?? [] as row (row.id)}
            <li class="row">
              <StatusDot tone={row.tone} />
              <Text as="span" size="xs" mono truncate title={row.outcome ? `${row.label} · ${row.outcome}` : row.label}>
                {row.label}
                {#if row.outcome}<span class="bb-sr-only">{row.outcome}</span>{/if}
              </Text>
              <div class="when" title={row.when}>
                <Text as="div" size="xs" mono tone="muted">
                  <bb-fit><span data-fit>{row.when}</span></bb-fit>
                </Text>
              </div>
            </li>
          {/each}
        </ul>
      </Scroller>
    {/if}
  </div>

  {#if more}
    <div class="more" class:quiet={!hasRows || Boolean(error)} inert={!hasRows || Boolean(error)}>
      <Text size="xs"><TextLink variant="inline" href={more.href}>{more.label}</TextLink></Text>
    </div>
  {/if}
</Stack>

<style>
  .history {
    block-size: 6rem;
  }

  .error {
    overflow-wrap: anywhere;
  }

  .rows {
    display: grid;
    gap: var(--bb-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) 6.5rem;
    gap: var(--bb-space-2);
    align-items: center;
    min-block-size: 1rem;
  }

  .when {
    overflow: hidden;
    text-align: end;
  }

  .more {
    transition: opacity var(--bb-dur-fast) ease;
  }

  .quiet {
    opacity: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .more {
      transition: none;
    }
  }
</style>
