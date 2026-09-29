<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import { Text, getI18n } from '@bagel/kit';

  let {
    title,
    body,
    button,
    color,
    caption,
    limits = []
  }: {
    title: string;
    body: string;
    button: string;
    color: string;
    caption: string;
    limits?: { label: string; n: number; max: number }[];
  } = $props();

  const { t } = getI18n();
</script>

<figure class="embed-figure">
  <figcaption class="embed-caption">{caption}</figcaption>
  <div class="embed" style="border-left-color: {color}">
    <p class="embed-title">{title}</p>
    <p class="embed-body">{body}</p>
    <div class="embed-actions">
      <span class="embed-button">{button}</span>
    </div>
  </div>
  {#if limits.length}
    <p class="embed-counts">
      {#each limits as limit (limit.label)}
        <Text as="span" size="xs" mono tone={limit.n >= limit.max ? 'accent' : 'muted'}>{limit.label} {t('discord.embedCount', { n: limit.n, max: limit.max })}</Text>
      {/each}
    </p>
  {/if}
</figure>

<style>
  .embed-figure { margin: 0; }
  .embed-caption {
    font-family: var(--bb-font-body);
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0 0 8px;
  }

  .embed {
    border-left: 4px solid var(--bb-tan);
    border-radius: var(--bb-radius-sm);
    background: rgba(var(--bb-white-rgb), 0.04);
    padding: 12px 14px;
    max-width: 440px;
  }
  .embed-title {
    margin: 0 0 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14px;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .embed-body {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-white);
    opacity: 0.82;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .embed-counts {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 16px;
    margin: 8px 0 0;
  }
  .embed-actions { margin-top: 12px; }
  .embed-button {
    display: inline-block;
    padding: 7px 14px;
    border-radius: var(--bb-radius-sm);
    border: 1px solid var(--bb-glass-border);
    background: rgba(var(--bb-white-rgb), 0.08);
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-white);
    max-width: 100%;
    overflow-wrap: anywhere;
  }
</style>
