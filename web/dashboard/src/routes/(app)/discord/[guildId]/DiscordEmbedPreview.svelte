<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  let {
    title,
    body,
    button,
    color,
    caption
  }: { title: string; body: string; button: string; color: string; caption: string } = $props();
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
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
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
  .embed-actions { margin-top: 12px; }
  .embed-button {
    display: inline-block;
    padding: 7px 14px;
    border-radius: 6px;
    border: 1px solid var(--glass-border);
    background: rgba(240, 236, 228, 0.08);
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-white);
    max-width: 100%;
    overflow-wrap: anywhere;
  }
</style>
