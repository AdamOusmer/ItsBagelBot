<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';
  import { getUiI18n } from './i18n';
  import { flagBody } from '../lib/flags';
  import { mountLangSwitch } from '../lib/lang-switch';
  import '../styles/elements/nav.css';

  const i18n = getUiI18n();
  type LocaleChoice = { code: string; label: string; href?: string; current?: boolean; title?: string; flag?: string };

  type Own = {
    options: { code: string; label: string; href?: string; current?: boolean; title?: string; flag?: string }[];
    label?: string;
    action?: string;
    name?: string;
    fields?: Record<string, string>;
    onSelect?: (code: string) => void;
    variant?: 'compact' | 'field';
    menuId?: string;
    class?: string;
  };

  let {
    options,
    label = i18n.t('nav.language'),
    action,
    name = 'locale',
    fields = {},
    onSelect,
    variant = 'compact',
    menuId,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const fallbackId = $props.id();
  const popoverId = $derived(menuId ?? `${fallbackId}-lang`);
  const current = $derived(options.find((option) => option.current) ?? options[0]);
  const classes = $derived(
    ['bb-lang-switch', variant === 'field' ? 'bb-lang-switch--field' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
  const optClass = (option: LocaleChoice) =>
    option.current ? 'bb-lang-switch__opt is-active' : 'bb-lang-switch__opt';
</script>

{#snippet flag(code: string | undefined)}<span class="bb-lang-switch__flag" aria-hidden="true"
    >{#if flagBody(code)}<svg viewBox="0 0 512 512">{@html flagBody(code)}</svg>{/if}</span
  >{/snippet}

{#snippet row(option: LocaleChoice)}{@render flag(option.flag)}<span class="bb-lang-switch__name" lang={option.code}
    >{option.title ?? option.label}</span
  ><Icon name="check" size={14} class="bb-lang-switch__check" />{/snippet}

{#snippet body()}<button
    type="button"
    class="bb-lang-switch__trigger"
    popovertarget={popoverId}
    aria-label="{label}: {current?.title ?? current?.label}"
    >{@render flag(current?.flag)}<span class="bb-lang-switch__label"
      >{variant === 'field' ? (current?.title ?? current?.label) : current?.label}</span
    ><Icon name="chevron" size={12} class="bb-lang-switch__chevron" /></button
  ><ul class="bb-lang-switch__menu" id={popoverId} popover="auto" role="list" aria-label={label}
    >{#each options as option (option.code)}<li
        >{#if action}<button
            type="submit"
            {name}
            value={option.code}
            class={optClass(option)}
            aria-pressed={option.current ? 'true' : 'false'}>{@render row(option)}</button
          >{:else if onSelect}<button
            type="button"
            class={optClass(option)}
            aria-pressed={option.current ? 'true' : 'false'}
            onclick={() => onSelect(option.code)}>{@render row(option)}</button
          >{:else}<a
            class={optClass(option)}
            href={option.href}
            hreflang={option.code}
            aria-current={option.current ? 'true' : undefined}>{@render row(option)}</a
          >{/if}</li
      >{/each}</ul
  >{/snippet}

{#if action}<form method="POST" {action} class={classes} data-bb-lang-switch="" {...rest} {@attach mountLangSwitch}
    >{#each Object.entries(fields) as [field, fieldValue] (field)}<input
        type="hidden"
        name={field}
        value={fieldValue}
      />{/each}{@render body()}</form
  >{:else}<div class={classes} data-bb-lang-switch="" {...rest} {@attach mountLangSwitch}>{@render body()}</div>{/if}
