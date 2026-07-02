<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  let {
    icon = undefined as IconName | undefined,
    title,
    body = undefined as string | undefined,
    children = undefined as Snippet | undefined
  }: {
    icon?: IconName;
    title: string;
    body?: string;
    children?: Snippet;
  } = $props();
</script>

<div class="empty">
  {#if icon}<div class="glyph"><Icon name={icon} size={22} /></div>{/if}
  <p class="title">{title}</p>
  {#if body}<p class="body">{body}</p>{/if}
  {#if children}<div class="cta">{@render children()}</div>{/if}
</div>

<style>
  .empty {
    padding: 36px 20px;
    text-align: center;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
  }
  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 46px;
    height: 46px;
    margin-bottom: 12px;
    border-radius: 50%;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.08);
    border: 1px solid rgba(201, 168, 124, 0.22);
  }
  .title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 17px;
    color: var(--bb-white);
    margin: 0 0 6px;
    letter-spacing: -0.01em;
  }
  .body { font-size: 13px; line-height: 1.5; margin: 0 auto; max-width: 42ch; }
  .cta { margin-top: 16px; display: flex; justify-content: center; gap: 10px; flex-wrap: wrap; }
</style>
