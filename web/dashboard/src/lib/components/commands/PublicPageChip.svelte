<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n } from '@bagel/kit';
  import { copyText } from '@bagel/ui/lib/clipboard';

  const { t } = getI18n();

  let { on, url }: { on: boolean; url: string } = $props();

  async function copy() {
    const ok = await copyText(url, { legacyFallback: true });
    toast(ok ? 'success' : 'danger', t(ok ? 'commands.publicPageCopied' : 'commands.publicPageCopyFailed'));
  }
</script>

{#if on && url}
  <div class="pp">
    <span class="pp-label"><Icon name="link" size={12} /><Text as="span" size="xs" tone="success">{t('commands.publicPageOn')}</Text></span>
    <button type="button" class="pp-act" onclick={copy}>{t('commands.publicPageCopy')}</button>
    <a class="pp-act" href={url} target="_blank" rel="noopener">{t('commands.publicPageOpen')}</a>
  </div>
{:else if !on}
  <div class="pp">
    <a class="pp-act pp-off" href="/settings#public-pages" title={t('commands.publicPageOffTitle')}>
      <Icon name="link" size={12} />{t('commands.publicPageOff')}
    </a>
  </div>
{/if}

<style>
  .pp {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
    min-height: 28px;
    font-family: var(--bb-font-body);
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
  }
  .pp-label { display: inline-flex; align-items: center; gap: 6px; color: var(--bb-green-glow); }
  .pp-act {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    color: var(--bb-muted);
    text-decoration: underline;
    cursor: pointer;
  }
  .pp-act:hover { color: var(--bb-white); }
  .pp-act:focus-visible { outline: 2px solid var(--bb-tan); outline-offset: 2px; }
  @media (pointer: coarse) {
    .pp-act { min-height: 44px; }
  }
</style>
