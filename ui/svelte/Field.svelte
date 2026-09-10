<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Labelled form-field wrapper. The caller passes the actual control (an
  // <input class="bb-input">, a <select>, a SearchInput) as children, so the
  // binding stays at the call site and this stays pure.
  //
  // `hintId` / `errorId` are caller-supplied rather than generated with
  // $props.id(). Two reasons: the caller is the one that has to put the same
  // id in the control's aria-describedby, and a generated id has no Astro
  // twin, so the parity test could never compare the two adapters.
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
    /** Muted suffix, e.g. "default: 5". */
    tag?: string;
    hint?: string;
    error?: string;
    hintId?: string;
    errorId?: string;
    /** Explicit association; omit to rely on the wrapping <label>. */
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
