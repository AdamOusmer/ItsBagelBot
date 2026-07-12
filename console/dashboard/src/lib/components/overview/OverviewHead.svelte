<script lang="ts">
  // Compact page head for Overview. Mirrors the shared PageHead idiom (mono
  // green eyebrow, display title, muted lede) but ships the one <h1> for the
  // route WITH tabindex="-1" so skip-to-content / route changes can move focus
  // straight to the page title. PageHead can't take tabindex on its heading, so
  // this keeps the same look while owning the focus target.
  let {
    eyebrow,
    greeting,
    channel,
    description
  }: {
    eyebrow: string;
    greeting: string;
    channel: string;
    description: string;
  } = $props();
</script>

<div class="ov-head">
  <span class="eyebrow">{eyebrow}</span>
  <h1 tabindex="-1">{greeting}, <em>{channel}</em></h1>
  <p>{description}</p>
</div>

<style>
  /* Compact vs PageHead: smaller title clamp and tighter spacing, since the bot
     status panel — not the greeting — is the visual anchor of this page. */
  .ov-head {
    margin-bottom: calc(22px * var(--d));
  }
  .eyebrow {
    font-family: var(--bb-font-mono);
    font-size: 0.72rem;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    display: block;
    margin-bottom: 10px;
  }
  h1 {
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(26px, 3vw, 36px);
    line-height: 1.06;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0;
  }
  /* Focus lands here from the skip link; the h1 is a target, not a control, so
     no ring (matches AppShell's <main> convention). */
  h1:focus {
    outline: none;
  }
  h1 em {
    font-style: normal;
    font-weight: 800;
    color: var(--bb-tan-light);
  }
  p {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 8px 0 0;
    max-width: 560px;
  }
</style>
