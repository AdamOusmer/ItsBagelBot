<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/copy-surface.css';
  import { onDestroy, type Snippet } from 'svelte';
  import { copyText } from '../lib/clipboard';
  import Icon from './Icon.svelte';

  let {
    text,
    copiedLabel,
    variant = 'card',
    label,
    hint = '',
    flashMs,
    legacyFallback = false,
    oncopy,
    class: className = '',
    children,
    ...rest
  }: {
    text: string;
    copiedLabel: string;
    variant?: 'card' | 'row' | 'well';
    label?: string;
    hint?: string;
    flashMs?: number;
    legacyFallback?: boolean;
    oncopy?: (copied: boolean) => void;
    class?: string;
    children?: Snippet<[boolean]>;
    [key: string]: unknown;
  } = $props();

  const FLASH_MS = 1600;

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const classes = $derived(
    ['bb-copy', `bb-copy--${variant}`, className || null].filter(Boolean).join(' '),
  );
  const glyphs = $derived(variant === 'well');

  onDestroy(() => clearTimeout(timer));

  async function copy() {
    const ok = await copyText(text, { legacyFallback });
    oncopy?.(ok);
    if (!ok) return;
    clearTimeout(timer);
    copied = true;
    timer = setTimeout(() => (copied = false), flashMs ?? FLASH_MS);
  }
</script>

<button
  class={classes}
  type="button"
  data-copy={text}
  data-copy-ms={flashMs}
  data-copy-legacy={legacyFallback ? '' : undefined}
  data-copied={copied ? '' : undefined}
  onclick={copy}
  {...rest}
  >{#if label}<span class="bb-copy__label">{label}</span>{/if}<span class="bb-copy__row"
    ><span class="bb-copy__value">{#if children}{@render children(copied)}{:else}{text}{/if}</span
    ><span class="bb-copy__hint" aria-hidden="true"
      ><span class="bb-copy__idle">{#if glyphs}<Icon name="copy" size={12} />{/if}{hint}</span
      ><span class="bb-copy__done">{#if glyphs}<Icon name="check" size={12} />{/if}{copiedLabel}</span
      ></span
    ></span
  ><span class="bb-copy__status" role="status">{copied ? copiedLabel : ''}</span></button
>
