<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';

  // type defaults to "button", NOT the HTML default "submit". Every Chip in
  // this console is an in-page toggle or a label, and several sit inside the
  // one big <form> a settings page posts: with the HTML default, clicking a
  // category chip to REMOVE it also submitted the whole form. That shipped.
  // Callers that genuinely want a submit chip pass type="submit" explicitly,
  // and the Astro twin hard-codes type="button" for the same reason.
  //
  // `on` is emitted as [data-on], not the old .is-on class: a Svelte
  // `class:is-on` has no Astro spelling the parity normaliser can match, while
  // both frameworks collapse an empty data attribute to the bare form.
  let {
    on = false,
    onclick,
    type = 'button',
    tone = undefined,
    class: cls = '',
    children,
    ...rest
  }: {
    on?: boolean;
    onclick?: () => void;
    type?: 'button' | 'submit' | 'reset';
    tone?: 'muted' | 'danger' | 'eyebrow';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<button
  {type}
  class="bb-chip{tone ? ` bb-chip--${tone}` : ''}{cls ? ` ${cls}` : ''}"
  data-on={on ? '' : undefined}
  {onclick}
  {...rest}
>{@render children()}</button>
