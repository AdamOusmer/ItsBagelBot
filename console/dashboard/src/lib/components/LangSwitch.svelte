<script lang="ts">
  // Compact EN/FR toggle. Posts to /lang (plain form, no fetch) with the current
  // path as `next` so the switch keeps you on the same page in the new language.
  import { page } from '$app/state';
  import { getI18n, LOCALES, type Locale } from '@bagel/shared';

  let { selected }: { selected?: Locale } = $props();
  const i18n = getI18n();
  const active = $derived(selected ?? i18n.locale);
  const next = $derived(page.url.pathname + page.url.search);
  const short = (l: Locale) => l.toUpperCase();
</script>

<form method="POST" action="/lang" class="lang" aria-label={i18n.t('lang.switchAria')}>
  <input type="hidden" name="next" value={next} />
  {#each LOCALES as l (l)}
    <button
      type="submit"
      name="to"
      value={l}
      class="lang-opt"
      class:active={l === active}
      aria-pressed={l === active}
      title={i18n.t(`lang.${l}`)}
    >{short(l)}</button>
  {/each}
</form>

<style>
  .lang {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    border-radius: var(--bb-radius-pill, 100px);
    flex: none;
  }
  .lang-opt {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--bb-muted);
    background: none;
    border: none;
    cursor: pointer;
    padding: 5px 9px;
    border-radius: var(--bb-radius-pill, 100px);
    transition: color var(--bb-dur-fast, 160ms) ease, background var(--bb-dur-fast, 160ms) ease;
  }
  .lang-opt:hover { color: var(--bb-tan-pale); }
  .lang-opt.active {
    color: #0a0a0a;
    background: var(--bb-tan);
  }
</style>
