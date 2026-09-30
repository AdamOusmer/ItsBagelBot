<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/copy-surface.css';
  import { onDestroy, type Snippet } from 'svelte';
  import { copyText } from '../lib/clipboard';
  import { getUiI18n } from './i18n';
  import Icon from './Icon.svelte';

  type Own = {
    text: string;
    copiedLabel?: string;
    announce?: string;
    variant?: 'card' | 'row' | 'well';
    label?: string;
    hint?: string;
    flashMs?: number;
    legacyFallback?: boolean;
    onCopy?: (copied: boolean) => void;
    class?: string;
    children?: Snippet<[boolean]>;
  };

  const i18n = getUiI18n();
  let {
    text,
    copiedLabel = i18n.t('action.copied'),
    announce,
    variant = 'card',
    label,
    hint = i18n.t('action.copy'),
    flashMs,
    legacyFallback = false,
    onCopy,
    class: className = '',
    children,
    onclick: userClick,
    ...rest
  }: Own & Omit<SvelteHTMLElements['button'], keyof Own> = $props();

  const FLASH_MS = 1600;

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  const classes = $derived(
    ['bb-copy', `bb-copy--${variant}`, className || null].filter(Boolean).join(' '),
  );
  const glyphs = $derived(variant === 'well');

  onDestroy(() => clearTimeout(timer));

  function onclick(event: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) {
    userClick?.(event);
    void copy();
  }

  async function copy() {
    const ok = await copyText(text, { legacyFallback });
    onCopy?.(ok);
    if (!ok) return;
    clearTimeout(timer);
    copied = true;
    timer = setTimeout(() => (copied = false), flashMs ?? FLASH_MS);
  }
</script>

<span class="bb-copy-host"
  ><button
    {...rest}
    class={classes}
    type="button"
    data-copy={text}
    data-copy-announce={announce}
    data-copy-ms={flashMs}
    data-copy-legacy={legacyFallback ? '' : undefined}
    data-copied={copied ? '' : undefined}
    {onclick}
    >{#if label}<span class="bb-copy__label">{label}</span>{/if}<span class="bb-copy__row"
      ><span class="bb-copy__value">{#if children}{@render children(copied)}{:else}{text}{/if}</span
      ><span class="bb-copy__hint" aria-hidden="true"
        ><span class="bb-copy__idle">{#if glyphs}<Icon name="copy" size={12} />{/if}{hint}</span
        ><span class="bb-copy__done">{#if glyphs}<Icon name="check" size={12} />{/if}{copiedLabel}</span
        ></span
      ></span
    ></button
  ><span class="bb-copy__status" role="status">{copied ? (announce ?? copiedLabel) : ''}</span></span
>
