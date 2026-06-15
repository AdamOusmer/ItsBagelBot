<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let {
    variant = 'ghost',
    icon,
    type = 'button',
    onclick,
    children,
    ...rest
  }: {
    variant?: 'primary' | 'ghost' | 'tan';
    icon?: IconName;
    type?: 'button' | 'submit';
    onclick?: () => void;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<button class="btn {variant}" {type} {onclick} {...rest}>
  {#if icon}<Icon name={icon} size={14} />{/if}
  {@render children()}
</button>
