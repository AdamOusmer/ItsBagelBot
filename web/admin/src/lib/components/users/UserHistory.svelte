<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
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
  const showMore = $derived(hasRows && !error);
</script>

<Stack as="section" gap={2}>
  <Heading id={heading} level={3} variant="label">{title}</Heading>
  {#if pending}<VisuallyHidden role="status">{loading}</VisuallyHidden>{/if}
  <div class="history" style:--history-rows={VISIBLE_ROWS}>
    <div aria-busy={pending}>
      {#if pending}
        <SkeletonStack rows={VISIBLE_ROWS} height="var(--history-row-size)" />
      {:else if error}
        <div class="error">
          <AlertBanner tone="danger" flush>{error}</AlertBanner>
        </div>
      {:else if !hasRows}
        <EmptyState style="--empty-pad: var(--bb-space-6) 0" title={empty} />
      {:else}
        <ul class="rows">
          {#each rows ?? [] as row (row.id)}
            <li class="row">
              <StatusDot tone={row.tone} />
              <Text as="span" size="xs" mono truncate title={row.outcome ? `${row.label} · ${row.outcome}` : row.label}>
                {row.label}
                {#if row.outcome}<VisuallyHidden>{row.outcome}</VisuallyHidden>{/if}
              </Text>
              <div class="when" title={row.when}>
                <Text as="div" size="xs" mono tone="muted">
                  <bb-fit><span data-fit>{row.when}</span></bb-fit>
                </Text>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>

  {#if more}
    <div class="more" class:quiet={!showMore} inert={!showMore}>
      <Text size="xs">
        <bb-fit><TextLink variant="inline" href={more.href}><span data-fit>{more.label}</span></TextLink></bb-fit>
      </Text>
    </div>
  {/if}
</Stack>

<style>
  .history {
    --history-row-size: 1.25rem;
    --history-row-gap: 10px;
    min-block-size: calc(var(--history-row-size) * var(--history-rows) + var(--history-row-gap) * (var(--history-rows) - 1));
  }

  .error {
    overflow-wrap: anywhere;
  }

  .rows {
    display: grid;
    gap: var(--history-row-gap);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) 6.5rem;
    gap: var(--bb-space-2);
    align-items: center;
    block-size: var(--history-row-size);
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
