<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Generic status label on the shared tag vocabulary (@bagel/ui/styles/tags.css
  // + elements/badge.css). It knows tones, marks and the sweep; it does not
  // know what a permission is.
  //
  // The bot's permission ladder used to live inside this component as six
  // scoped `.t-<perm>` rules, which made a design primitive depend on a bot
  // domain type and shipped six bot-specific class names to every consumer of
  // the library. That half is @bagel/kit's PermBadge.svelte now, and it drives
  // the colour through --badge-tone. Same pixels, one direction of dependency.
  import type { Snippet } from 'svelte';

  let {
    tone = undefined,
    mark = undefined,
    sweep = false,
    dashed = false,
    status = false,
    children,
    class: className = '',
    ...rest
  }: {
    /** Tag vocabulary tone. Omit for a caller-driven --badge-tone. */
    tone?: 'quiet' | 'live' | 'alpha' | 'pre' | 'incoming' | 'bare';
    /** The 5px rotated-square dot family. */
    mark?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    /** The travelling hairline light. Never a pulse. */
    sweep?: boolean;
    dashed?: boolean;
    /** role="status": announce changes to this label as they happen. */
    status?: boolean;
    children: Snippet;
    /** Merged, not substituted: a caller adding a row-fit class must not be
        able to drop the contract's own by passing `class`. */
    class?: string;
    [key: string]: unknown;
  } = $props();
</script>

<span
  class="bb-tag bb-badge{tone ? ` bb-tag--${tone}` : ''}{dashed
    ? ' bb-badge--dashed'
    : ''}{className ? ` ${className}` : ''}"
  role={status ? 'status' : undefined}
  {...rest}
>{#if mark}<i class="bb-mark{mark === 'solid' ? '' : ` bb-mark--${mark}`}" aria-hidden="true"></i>{/if}{@render children()}{#if sweep}<i class="bb-sweep" aria-hidden="true"></i>{/if}</span>
