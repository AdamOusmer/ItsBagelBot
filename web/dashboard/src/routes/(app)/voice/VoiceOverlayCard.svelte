<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { copyText } from '@bagel/ui/lib/clipboard';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { configured, url, error, busy, disabled, submit }: {
    configured: boolean;
    url?: string;
    error?: string;
    busy: boolean;
    disabled: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  let copyResult = $state<{ url: string; copied: boolean }>();
  const copied = $derived(copyResult?.url === url && copyResult?.copied === true);
  const status = $derived(error ?? (copyResult?.url === url && copyResult?.copied === false
    ? t('voice.overlayCopyFailed') : ''));

  async function copyLink() {
    if (!url) return;
    const link = url;
    copyResult = { url: link, copied: await copyText(link, { legacyFallback: true }) };
  }
</script>

<Card as="section" aria-labelledby="voice-overlay-title">
  <div class="overlay">
    <Heading level={6} as="h2" id="voice-overlay-title">
      <bb-fit><span data-fit="">{t('voice.overlayTitle')}</span></bb-fit>
    </Heading>
    <div class="overlay-copy bb-fit-box">
      <Text as="div" size="sm" tone="muted">
        <bb-fit mode="block">{t('voice.overlayHint')}</bb-fit>
      </Text>
    </div>
    <form method="POST" action="?/rotateOverlay" use:enhance={submit}>
      <div class="overlay-actions">
        <Button variant="secondary" size="sm" type="submit" {busy} {disabled}>
          <bb-fit><span data-fit="">
            {configured || url ? t('voice.overlayReplace') : t('voice.overlayCreate')}
          </span></bb-fit>
        </Button>
        <div class="overlay-status bb-fit-box" role="status">
          <Text as="div" size="sm" tone="danger"><bb-fit mode="block">{busy ? '' : status}</bb-fit></Text>
        </div>
      </div>
    </form>
    <div class="overlay-link" class:available={!!url} inert={!url}>
      <Field label={t('voice.overlayLink')} fitCopy>
        <Input value={url ?? ''} readonly fill mono autocomplete="off" spellcheck={false} />
      </Field>
      <Button variant="secondary" size="sm" done={copied} disabled={busy} onclick={copyLink}>
        <bb-fit><span data-fit="">{copied ? t('common.copied') : t('common.copy')}</span></bb-fit>
      </Button>
    </div>
  </div>
</Card>

<style>
  .overlay {
    display: grid;
    gap: var(--bb-space-3);
    grid-template-columns: minmax(0, 1fr);
  }
  .overlay-copy, .overlay-status {
    block-size: 3rem;
    min-inline-size: 0;
    align-content: center;
  }
  .overlay-actions, .overlay-link {
    display: grid;
    gap: var(--bb-space-2);
    --btn-justify: center;
  }
  .overlay-actions {
    grid-template-columns: minmax(0, min(50%, 11rem)) minmax(0, 1fr);
    align-items: center;
    --btn-w: 100%;
  }
  .overlay-link {
    grid-template-columns: minmax(0, 1fr) minmax(0, 6rem);
    align-items: end;
    opacity: 0;
    --btn-w: 100%;
  }
  .overlay-link.available { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .overlay-link { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
