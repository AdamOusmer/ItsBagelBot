<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  // What the ticket panel will look like in Discord, drawn from the same
  // draft the editor is writing. Page-local on purpose: it imitates another
  // product's chrome (Discord's embed card), which is the opposite of what a
  // shared console primitive is for, and nothing else in the console should
  // grow a Discord-shaped surface by accident.
  //
  // The one colour that is not a token is the streamer's own embed colour. It
  // arrives as a validated #rrggbb from the shared config module and is
  // applied through a style ATTRIBUTE, which the console CSP allows
  // (style-src-attr 'unsafe-inline'); an inline stylesheet block would be
  // blocked, and a class per colour is impossible for a free-form value.
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

  /* Discord's embed card: a 4px accent rail on the left, a slightly lifted
     surface, tight title/description spacing, an action row underneath. */
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
