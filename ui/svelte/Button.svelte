<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import { buttonClass, hasMark, type ButtonSize, type ButtonTone, type ButtonVariant } from '../lib/button';
  import '../styles/elements/button.css';

  type Own = {
    variant?: ButtonVariant;
    tone?: ButtonTone;
    size?: ButtonSize;
    block?: boolean;
    as?: 'button' | 'span';
    type?: 'button' | 'submit' | 'reset';
    busy?: boolean;
    done?: boolean;
    disabled?: boolean;
    class?: string;
    children?: Snippet;
  };

  let {
    variant = 'primary',
    tone = 'neutral',
    size = 'md',
    block = false,
    as = 'button',
    type = 'button',
    busy = false,
    done = false,
    disabled = false,
    class: className,
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  const isStatic = $derived(as === 'span');
  const control = $derived(
    isStatic ? {} : { type, disabled: disabled || busy, 'aria-busy': busy ? ('true' as const) : undefined },
  );
</script>

<svelte:element
  this={as}
  class={[buttonClass({ variant, tone, size, block, busy, done, static: isStatic }), className]}
  {...control}
  data-mark=""
  {...rest}
>
  {#if hasMark(variant)}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{@render children?.()}</span>
  {#if busy}<span class="bb-btn__spinner" aria-hidden="true"></span>{/if}
</svelte:element>
