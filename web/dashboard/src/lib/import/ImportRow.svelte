<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { RowKind } from './helpers';
  import type { ImportSession } from './session.svelte';

  let { session, kind, index, text, label, ariaLabel, collided = false, body, chips }: {
    session: ImportSession;
    kind: RowKind;
    index: number;
    text: { cannotImport: (message: string) => string; alreadyExists: () => string };
    label?: string;
    ariaLabel?: string;
    collided?: boolean;
    body: Snippet;
    chips?: Snippet;
  } = $props();

  const diags = $derived(session.diagsFor(kind, index));
  const warnings = $derived(diags.filter((d) => d.severity === 'warn'));
  const errors = $derived(diags.filter((d) => d.severity === 'error'));
</script>

<li class="row-item" class:collision={collided}>
  <span class="pick">
    {#if label}
      <Checkbox bind:checked={() => session.isPicked(kind, index), (on) => session.toggle(kind, index, on)}><Text as="span" size="sm" mono>{label}</Text></Checkbox>
    {:else}
      <Checkbox bind:checked={() => session.isPicked(kind, index), (on) => session.toggle(kind, index, on)} aria-label={ariaLabel} />
    {/if}
  </span>
  <div class="row-body">
    {@render body()}
    <span class="chips">
      {@render chips?.()}
      {#each warnings as d (d.code + d.message)}
        <Tag tone="alpha" title={d.message}>{d.message}</Tag>
      {/each}
      {#each errors as d (d.code + d.message)}
        <Tag tone="danger" title={d.message}>{text.cannotImport(d.message)}</Tag>
      {/each}
      {#if collided}<Tag tone="danger">{text.alreadyExists()}</Tag>{/if}
    </span>
  </div>
</li>

<style>
  .row-item {
    display: flex;
    align-items: baseline;
    gap: 14px;
    border-radius: var(--bb-radius-md);
    padding: 16px 22px;
    background: var(--bb-elev-2);
    box-shadow: var(--bb-depth-1);
  }
  .row-item.collision {
    background: color-mix(in srgb, rgb(var(--bb-status-danger-border-rgb)) 14%, var(--bb-elev-2));
  }
  .pick {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
    --bb-check-align: center;
  }
  .row-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
</style>
