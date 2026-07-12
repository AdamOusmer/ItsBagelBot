<script lang="ts">
  // Local, in-page section navigation for the Settings page. Real <a href="#id">
  // anchors (NOT ARIA tabs): each one scrolls to and focuses a <section> whose
  // tabindex=-1 + scroll-margin-top lands it below the sticky topbar. Plain
  // links keep native semantics (open in new tab, right-click, "link" role).
  let {
    label,
    items
  }: {
    label: string;
    items: { href: string; label: string }[];
  } = $props();
</script>

<nav class="settings-nav" aria-label={label}>
  {#each items as item (item.href)}
    <a href={item.href}>{item.label}</a>
  {/each}
</nav>

<style>
  .settings-nav {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 22px;
  }
  .settings-nav a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 8px 16px;
    border-radius: var(--bb-radius-pill, 100px);
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    text-decoration: none;
    transition:
      color var(--bb-dur-fast, 160ms) ease,
      background var(--bb-dur-fast, 160ms) ease,
      border-color var(--bb-dur-fast, 160ms) ease;
  }
  .settings-nav a:hover {
    color: var(--bb-tan-pale);
    background: rgba(201, 168, 124, 0.08);
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
  }
  /* The linked section flashes its label so a keyboard user can see where the
     jump landed, without relying on colour alone (the focus ring also shows). */
  .settings-nav a:target {
    color: var(--bb-tan-pale);
  }
</style>
