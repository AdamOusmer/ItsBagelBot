<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/tag.css';
  import '../styles/elements/field.css';
  import '../styles/elements/fit.css';
  import type { Snippet } from 'svelte';

  type Own = {
    label: string;
    fitCopy?: boolean;
    tag?: string;
    hint?: string;
    error?: string;
    hintId?: string;
    errorId?: string;
    for?: string;
    fit?: boolean;
    children: Snippet;
  };

  let {
    label,
    fitCopy = false,
    tag,
    hint,
    error,
    hintId,
    errorId,
    for: htmlFor,
    fit = false,
    children,
    class: className,
    ...rest
  }: Own & Omit<SvelteHTMLElements['label'], keyof Own> = $props();
</script>

<label class={['bb-field', className]} for={htmlFor} {...rest}>
  <span class="bb-field__label">{#if fit || fitCopy}<span class="bb-field__text bb-fit-box"><bb-fit mode="block">{label}</bb-fit></span>{:else}{label}{/if}{#if tag}<small class="bb-tag bb-tag--quiet bb-field__tag">{tag}</small>{/if}</span>
  {@render children()}
  {#if hint}<small class="bb-field__hint" id={hintId}>{hint}</small>{/if}
  {#if error}<small class="bb-field__error" role="alert" id={errorId}>{error}</small>{/if}
</label>
