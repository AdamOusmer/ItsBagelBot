<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { customCursor } from '@bagel/kit/cursor';
  import { ensureCatalog, isLocale, localeName, LOCALES, type Locale } from '@bagel/kit/i18n';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { localeFlag } from '@bagel/kit/site-links';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import LanguageSwitcher from '@bagel/ui/svelte/LanguageSwitcher.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';

  const { t, locale } = getI18n();
  const options = LOCALES.map((code) => ({
    code,
    label: code.toUpperCase(),
    title: localeName(code),
    flag: localeFlag(code),
    current: code === locale
  }));
  const next = $derived.by(() => {
    const url = new URL(page.url.href);
    url.searchParams.delete('lang');
    return url.pathname + url.search + url.hash;
  });

  let pending = $state<'language' | 'cursor' | null>(null);

  async function postLanguage(action: string, body: FormData, choice: Locale) {
    if (!(await ensureCatalog(choice))) throw new Error();
    const response = await fetch(action, { method: 'POST', body, redirect: 'manual' });
    if (response.type !== 'opaqueredirect') throw new Error();
  }

  async function saveLanguage(event: SubmitEvent) {
    event.preventDefault();
    if (pending) return;
    const form = event.currentTarget as HTMLFormElement;
    const body = new FormData(form, event.submitter);
    const choice = body.get('to');
    if (!isLocale(choice)) return;
    const returnTo = next;
    const pathname = page.url.pathname;
    pending = 'language';
    try {
      await postLanguage(form.action, body, choice);
      if (page.url.pathname !== pathname) return;
      await goto(returnTo, { refreshAll: true, reset: false, replace: true });
      if (page.url.pathname === pathname && page.data.locale !== choice) throw new Error();
    } catch {
      toast('danger', t('admin.settings.languageSaveError'));
    } finally {
      pending = null;
    }
  }

  async function saveCursor(enabled: boolean) {
    pending = 'cursor';
    try {
      const response = await fetch('/cursor', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ enabled })
      });
      if (response.status !== 204) throw new Error();
    } catch {
      customCursor.set(!enabled);
      toast('danger', t('admin.settings.cursorSaveError'));
    } finally {
      pending = null;
    }
  }
</script>

<section class="screen active">
  <PageHead title={t('admin.settings.title')} />

  <div class="preferences">
    <Card>
      <div class="setting-row language-row">
        <div class="setting-copy">
          <Text size="sm"><bb-fit><span data-fit>{t('admin.settings.language')}</span></bb-fit></Text>
          <Text size="sm" tone="muted"><bb-fit><span data-fit>{t('admin.settings.languageHint')}</span></bb-fit></Text>
        </div>
        <LanguageSwitcher
          action="/lang"
          name="to"
          fields={{ next }}
          {options}
          label={t('admin.settings.language')}
          variant="field"
          aria-busy={pending === 'language'}
          inert={pending === 'cursor'}
          onsubmit={saveLanguage}
        />
      </div>
      <div class="setting-row">
        <div>
          <Text size="sm"><bb-fit><span data-fit>{t('admin.settings.customCursor')}</span></bb-fit></Text>
          <Text size="sm" tone="muted" id="cursor-hint"><bb-fit><span data-fit>{t('admin.settings.customCursorHint')}</span></bb-fit></Text>
        </div>
        <Switch
          bind:checked={$customCursor}
          label={t('admin.settings.customCursor')}
          describedby="cursor-hint"
          busy={pending === 'cursor'}
          disabled={pending === 'language'}
          onCheckedChange={saveCursor}
        />
      </div>
    </Card>
  </div>
</section>

<style>
  .preferences {
    max-inline-size: 48rem;
  }

  .setting-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
    min-block-size: 2.75rem;
  }

  .setting-row + .setting-row {
    border-block-start: 1px solid var(--bb-border);
    margin-block-start: var(--bb-space-4);
    padding-block-start: var(--bb-space-4);
    box-sizing: content-box;
  }

  @media (max-width: 600px) {
    .language-row {
      grid-template-columns: minmax(0, 1fr);
      justify-items: start;
    }

    .setting-copy {
      inline-size: 100%;
    }
  }
</style>
