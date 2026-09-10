<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the bare `.bb-tag` — tier 1 of the tag vocabulary
  // (../styles/tags.css). Astro twin: ../astro/Tag.astro.
  //
  // TAG VERSUS BADGE, since they render the same 11px mono uppercase label and
  // the difference is easy to lose: a Tag is a LABEL YOU READ whose tone comes
  // from the design system's own vocabulary (live / alpha / pre / quiet /
  // incoming / bare). A Badge is the same label with `.bb-badge` added, which
  // opens the `--badge-tone` seam so a CONSUMER can drive the colour from data
  // the library must not know about — the permission ladder being the case it
  // exists for. If the colour is one of the six the system names, this is the
  // element; if it comes from a domain type, that one is.
  //
  // Chip is the third and is not a label at all: it is a control you click.
  import '../styles/tags.css';
  import type { Snippet } from 'svelte';

  let {
    tone = undefined,
    mark = undefined,
    sweep = false,
    status = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: {
    /** Tag vocabulary tone. */
    tone?: 'quiet' | 'live' | 'alpha' | 'pre' | 'incoming' | 'bare' | 'error';
    /** The 5px rotated-square dot family. */
    mark?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    /** The travelling hairline light. Never a pulse. */
    sweep?: boolean;
    /** role="status": announce changes to this label as they happen. */
    status?: boolean;
    as?: 'span' | 'small' | 'div';
    /** Merged, not substituted: a caller must not be able to drop the
        contract's own class by passing `class`. */
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-tag', tone ? `bb-tag--${tone}` : null, className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:element this={tag} class={classes} role={status ? 'status' : undefined} {...rest}
  >{#if mark}<i
      class="bb-mark{mark === 'solid' ? '' : ` bb-mark--${mark}`}"
      aria-hidden="true"
    ></i>{/if}{@render children()}{#if sweep}<i class="bb-sweep" aria-hidden="true"></i>{/if}</svelte:element
>
