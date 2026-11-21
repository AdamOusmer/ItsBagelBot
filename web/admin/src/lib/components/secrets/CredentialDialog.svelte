<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import type { DbCredentialStatus } from '#lib/server/secrets';
  import { SECRET_DIALOGS, type SecretVerb } from './secret-dialogs';

  let { service, verb, onClose, onSaved }: {
    service: DbCredentialStatus;
    verb: SecretVerb;
    onClose: () => void;
    onSaved: () => Promise<void>;
  } = $props();

  const { t } = getI18n();
  const uid = $props.id();

  let confirmText = $state('');
  let dbUser = $state('');
  let busy = $state(false);
  let error = $state('');

  const dialog = $derived(SECRET_DIALOGS[verb]);
  const phrase = $derived(dialog.phrase(service, dbUser));
  const confirmReady = $derived(confirmText.trim() === phrase);
  const hintId = uid + '-confirm-hint';

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy || !confirmReady) {
      cancel();
      return;
    }
    busy = true;
    error = '';
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<{ action?: { ok: boolean; notice: string }; error?: string }>(result);
      if (result.type === 'success' && payload?.action?.ok) {
        toast('success', payload.action.notice);
        onClose();
        await onSaved();
        return;
      }
      error = payload?.error ?? t('admin.secrets.actionFailed');
    };
  };
</script>

<Modal
  open
  title={t(dialog.title)}
  describedBy={uid + '-description'}
  role={dialog.danger ? 'alertdialog' : 'dialog'}
  {busy}
  {onClose}
>
  <form method="POST" action={dialog.action} use:enhance={submit} class="credential-form">
    <input type="hidden" name="service" value={service.id} />
    <Stack gap={4}>
      <Text id={uid + '-description'} size="sm" tone="muted">
        {t(dialog.body, {
          schema: service.schema || 'DB_SCHEMA',
          project: service.project,
          config: service.config,
          service: service.label
        })}
      </Text>

      {#if dialog.needsUser}
        <Field label={t('admin.secrets.fieldDbUser')} fit>
          <Input
            name="db_user"
            fill
            mono
            required
            autocomplete="off"
            placeholder={service.expectedUserPrefix + '_…'}
            bind:value={dbUser}
          />
        </Field>
      {/if}

      {#if dialog.needsPassword}
        <Field label={t('admin.secrets.fieldDbPass')} fit>
          <Input name="db_pass" fill mono required minlength={32} type="password" autocomplete="new-password" />
        </Field>
      {/if}

      <Stack gap={1}>
        <Field label={t('admin.secrets.fieldConfirm', { phrase })} fit>
          <Input
            name="confirm"
            fill
            mono
            required
            autocomplete="off"
            autocapitalize="none"
            spellcheck={false}
            aria-describedby={hintId}
            bind:value={confirmText}
          />
        </Field>
        <div role="status" class="confirmation-feedback">
          <Text id={hintId} size="sm" tone={error ? 'danger' : 'muted'}>
            {error || t('admin.secrets.confirmHint')}
          </Text>
        </div>
      </Stack>

      <div class="dialog-actions">
        <Button block variant="ghost" onclick={onClose} disabled={busy}>
          <bb-fit><span data-fit>{t('common.cancel')}</span></bb-fit>
        </Button>
        <Button block type="submit" tone={dialog.danger ? 'danger' : 'neutral'} disabled={!confirmReady} {busy}>
          <bb-fit><span data-fit>{t(dialog.cta)}</span></bb-fit>
        </Button>
      </div>
    </Stack>
  </form>
</Modal>

<style>
  .credential-form {
    --field-mb: 0;
  }

  .confirmation-feedback {
    min-block-size: 3rem;
  }

  .dialog-actions {
    --btn-pad: 0 var(--bb-space-2);
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }
</style>
