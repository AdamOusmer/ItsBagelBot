<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/nav.css';

  type LocaleChoice = { code: string; label: string; href?: string; current?: boolean; title?: string };

  type Own = {
    options: { code: string; label: string; href?: string; current?: boolean; title?: string }[];
    ariaLabel: string;
    action?: string;
    name?: string;
    fields?: Record<string, string>;
    onselect?: (code: string) => void;
    class?: string;
  };

  let {
    options,
    ariaLabel,
    action,
    name = 'locale',
    fields = {},
    onselect,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-lang-switch', className || null].filter(Boolean).join(' '),
  );
</script>

{#snippet choice(option: LocaleChoice, type: 'submit' | 'button')}<button
    {type}
    name={type === 'submit' ? name : undefined}
    value={type === 'submit' ? option.code : undefined}
    class="bb-lang-switch__opt {option.current ? 'is-active' : ''}"
    aria-pressed={option.current ? 'true' : 'false'}
    title={option.title}
    onclick={type === 'button' ? () => onselect?.(option.code) : undefined}>{option.label}</button
  >{/snippet}

{#if action}<form method="POST" {action} class={classes} role="group" aria-label={ariaLabel} {...rest}
    >{#each Object.entries(fields) as [field, fieldValue] (field)}<input
        type="hidden"
        name={field}
        value={fieldValue}
      />{/each}{#each options as option (option.code)}{@render choice(option, 'submit')}{/each}</form
  >{:else if onselect}<div class={classes} role="group" aria-label={ariaLabel} {...rest}
    >{#each options as option (option.code)}{@render choice(option, 'button')}{/each}</div
  >{:else}<div class={classes} role="group" aria-label={ariaLabel} {...rest}
    >{#each options as option (option.code)}<a
        class="bb-lang-switch__opt {option.current ? 'is-active' : ''}"
        href={option.href}
        hreflang={option.code}
        aria-current={option.current ? 'true' : undefined}>{option.label}</a
      >{/each}</div
  >{/if}
