<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { copyText } from '@bagel/ui/lib/clipboard';
  import '@bagel/ui/styles/elements/collapse.css';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { onClose }: { onClose: () => void } = $props();

  const { t } = getI18n();
  const uid = $props.id();
  const options = [
    { value: 'base64', label: t('admin.secrets.genBase64') },
    { value: 'hex', label: t('admin.secrets.genHex') },
    { value: 'password', label: t('admin.secrets.genPassword') }
  ];
  const PASSWORD_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~!*';

  let format = $state('base64');
  let generated = $state('');
  let copyResult = $state<'copied' | 'failed' | null>(null);

  function generateValue(kind: string): string {
    if (kind === 'password') {
      const bytes = crypto.getRandomValues(new Uint8Array(40));
      return [...bytes].map((b) => PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length]).join('');
    }
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    if (kind === 'hex') return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    return btoa(String.fromCharCode(...bytes));
  }

  function selectFormat(next: string) {
    if (format === next) return;
    format = next;
    generated = '';
    copyResult = null;
  }

  function generate() {
    generated = generateValue(format);
    copyResult = null;
  }

  async function copyGenerated() {
    const value = generated;
    const copied = await copyText(value);
    if (generated === value) copyResult = copied ? 'copied' : 'failed';
  }
</script>

<Modal open title={t('admin.secrets.genTitle')} describedBy={uid + '-note'} {onClose}>
  <Stack gap={4}>
    <Text id={uid + '-note'} size="sm" tone="muted">{t('admin.secrets.genNote')}</Text>
    <Field label={t('admin.secrets.genFormat')} fit style="--field-mb: 0">
      <Select fill {options} bind:value={() => format, selectFormat} />
    </Field>
    <div role="status" class="bb-collapse" class:is-open={Boolean(generated)} style="--bb-collapse-gap: var(--bb-space-4)">
      <div>
        <Text size="sm" tone={copyResult === 'failed' ? 'danger' : 'muted'}>
          {#if copyResult === 'failed'}
            {t('admin.secrets.genCopyFailed')}
          {:else if copyResult === 'copied'}
            {t('common.copied')}
          {:else}
            {t('admin.secrets.generatedValue')}
          {/if}
        </Text>
      </div>
    </div>
    <div class="generator-actions">
      <Button block variant="ghost" onclick={onClose}>
        <bb-fit><span data-fit>{t('common.done')}</span></bb-fit>
      </Button>
      <Button block variant="secondary" onclick={generate}>
        <bb-fit><span data-fit>{t('admin.secrets.generate')}</span></bb-fit>
      </Button>
      <Button block onclick={copyGenerated} disabled={!generated}>
        <bb-fit><span data-fit>{t('common.copy')}</span></bb-fit>
      </Button>
    </div>
  </Stack>
</Modal>

<style>
  .generator-actions {
    --btn-pad: 0 var(--bb-space-2);
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }
</style>
