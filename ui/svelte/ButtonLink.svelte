<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import { buttonClass, hasMark, type ButtonSize, type ButtonTone, type ButtonVariant } from '../lib/button';
  import '../styles/elements/button.css';

  type Own = {
    href: string;
    variant?: ButtonVariant;
    tone?: ButtonTone;
    size?: ButtonSize;
    block?: boolean;
    done?: boolean;
    disabled?: boolean;
    class?: string;
    children?: Snippet;
  };

  let {
    href,
    variant = 'primary',
    tone = 'neutral',
    size = 'md',
    block = false,
    done = false,
    disabled = false,
    class: className,
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['a'], keyof Own> = $props();
</script>

<a
  class={[buttonClass({ variant, tone, size, block, done }), className]}
  href={disabled ? undefined : href}
  role={disabled ? 'link' : undefined}
  aria-disabled={disabled ? 'true' : undefined}
  data-mark=""
  {...rest}
>
  {#if hasMark(variant)}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{@render children?.()}</span>
</a>
