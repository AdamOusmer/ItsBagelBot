<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';

  let {
    label,
    tag,
    hint,
    error,
    hintId,
    errorId,
    for: htmlFor,
    children,
    ...rest
  }: {
    label: string;
    tag?: string;
    hint?: string;
    error?: string;
    hintId?: string;
    errorId?: string;
    for?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<label class="bb-field" for={htmlFor} data-invalid={error ? '' : undefined} {...rest}>
  <span class="bb-field__label">{label}{#if tag}<small class="bb-tag bb-tag--quiet bb-field__tag">{tag}</small>{/if}</span>
  {@render children()}
  {#if hint}<small class="bb-field__hint" id={hintId}>{hint}</small>{/if}
  {#if error}<small class="bb-field__error" role="alert" id={errorId}>{error}</small>{/if}
</label>
