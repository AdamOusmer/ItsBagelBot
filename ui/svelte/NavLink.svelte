<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-nav-link` contract. Its Astro twin is
  // ../astro/NavLink.astro and the two are held to identical markup by
  // ../test/parity.test.ts — the class list, the attribute order and the
  // element structure below are the contract, not an implementation detail.
  //
  // Knows about links. Never about ItsBagelBot: every string arrives as a
  // prop, including the locked hint, which the console used to resolve with an
  // i18n call inside the component.
  import '../styles/elements/nav-link.css';
  import type { Snippet } from 'svelte';

  let {
    href,
    label = '',
    current = false,
    variant = 'rail',
    external = false,
    block = false,
    disabled = false,
    hint,
    class: className = '',
    icon,
    trail,
    children,
    ...rest
  }: {
    /** Target. Omitted only for a `disabled` entry, which renders a span. */
    href?: string;
    /** Visible text. `children` wins when both are given. */
    label?: string;
    /** Current route: emits aria-current="page", which is what the CSS keys on. */
    current?: boolean;
    /** rail = hairline + drawline (default); cta = the one filled form. */
    variant?: 'rail' | 'cta';
    /** Opens in a new tab, with the rel that makes that safe. */
    external?: boolean;
    /** Full-width row form, for a stacked list rather than a bar. */
    block?: boolean;
    /** Non-interactive entry. Renders a span, so there is nothing to focus. */
    disabled?: boolean;
    /** sr-only text explaining a disabled entry. Caller-supplied wording. */
    hint?: string;
    class?: string;
    icon?: Snippet;
    /** Trailing content after the label: a count, a lock glyph, a badge. */
    trail?: Snippet;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  // Built once here and mirrored in the Astro adapter. Order matters: the
  // parity test does not sort attributes or classes, deliberately, so that a
  // diff of the two adapters' output is empty rather than merely equivalent.
  const classes = $derived(
    [
      'bb-nav-link',
      variant === 'cta' ? 'bb-nav-link--cta' : null,
      block ? 'bb-nav-link--block' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if disabled}
  <span class={classes} aria-disabled="true" {...rest}
    >{#if icon}{@render icon()}{/if}<span class="bb-nav-link__label"
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trail}{@render trail()}{/if}{#if hint}<span class="bb-nav-link__hint"
      >{hint}</span
    >{/if}</span
  >
{:else}
  <a
    class={classes}
    {href}
    aria-current={current ? 'page' : undefined}
    target={external ? '_blank' : undefined}
    rel={external ? 'noopener noreferrer' : undefined}
    {...rest}
    >{#if icon}{@render icon()}{/if}<span class="bb-nav-link__label"
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trail}{@render trail()}{/if}</a
  >
{/if}
