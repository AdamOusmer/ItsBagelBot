<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/nav.css';

  const i18n = getUiI18n();
  type LocaleChoice = { code: string; label: string; href?: string; current?: boolean; title?: string };

  type Own = {
    options: { code: string; label: string; href?: string; current?: boolean; title?: string }[];
    label?: string;
    action?: string;
    name?: string;
    fields?: Record<string, string>;
    onSelect?: (code: string) => void;
    class?: string;
  };

  let {
    options,
    label = i18n.t('nav.language'),
    action,
    name = 'locale',
    fields = {},
    onSelect,
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
    onclick={type === 'button' ? () => onSelect?.(option.code) : undefined}>{option.label}</button
  >{/snippet}

{#if action}<form method="POST" {action} class={classes} role="group" aria-label={label} {...rest}
    >{#each Object.entries(fields) as [field, fieldValue] (field)}<input
        type="hidden"
        name={field}
        value={fieldValue}
      />{/each}{#each options as option (option.code)}{@render choice(option, 'submit')}{/each}</form
  >{:else if onSelect}<div class={classes} role="group" aria-label={label} {...rest}
    >{#each options as option (option.code)}{@render choice(option, 'button')}{/each}</div
  >{:else}<div class={classes} role="group" aria-label={label} {...rest}
    >{#each options as option (option.code)}<a
        class="bb-lang-switch__opt {option.current ? 'is-active' : ''}"
        href={option.href}
        hreflang={option.code}
        aria-current={option.current ? 'true' : undefined}>{option.label}</a
      >{/each}</div
  >{/if}
