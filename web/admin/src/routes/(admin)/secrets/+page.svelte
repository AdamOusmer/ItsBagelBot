<script lang="ts">
  import Input from '@bagel/ui/svelte/Input.svelte';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure, copyFlash } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import type { DbCredentialStatus } from '$lib/server/secrets';
  import StatePill from '$lib/components/StatePill.svelte';
  import ServiceCard from '$lib/components/secrets/ServiceCard.svelte';
  import { SECRET_DIALOGS, type SecretVerb } from '$lib/components/secrets/secret-dialogs';
  import type { SecretsBundle } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canManage = $derived(allows(data.role, 'secrets.manage'));

  let bundle = $state<SecretsBundle | null>(null);
  $effect(() => {
    let alive = true;
    data.bundle.then((b: SecretsBundle) => {
      if (alive) bundle = b;
    });
    return () => {
      alive = false;
    };
  });

  const services = $derived(bundle?.services ?? []);

  let pendingVerb = $state<SecretVerb | null>(null);
  let pendingService = $state<DbCredentialStatus | null>(null);
  let confirmText = $state('');
  let dbUser = $state('');
  let dbPass = $state('');
  let busy = $state(false);
  let dialogForm = $state<HTMLFormElement | null>(null);

  const dialog = $derived(pendingVerb ? SECRET_DIALOGS[pendingVerb] : null);
  const phrase = $derived(dialog && pendingService ? dialog.phrase(pendingService, dbUser) : '');
  const phraseMatches = $derived(confirmText.trim() === phrase && phrase !== '');

  function open(verb: SecretVerb, service: DbCredentialStatus) {
    pendingVerb = verb;
    pendingService = service;
    confirmText = '';
    dbUser = '';
    dbPass = '';
  }

  function close() {
    pendingVerb = null;
    pendingService = null;
  }

  const dialogSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload<{ action?: { ok: boolean; notice: string }; error?: string }>(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        close();
        bundle = null;
        await invalidateAll();
        return;
      }
      failed(p, t('admin.secrets.actionFailed'));
    };
  };

  const GEN_KINDS = ['base64', 'hex', 'password'] as const;
  type GenKind = (typeof GEN_KINDS)[number];

  const GEN_LABEL = {
    base64: 'admin.secrets.genBase64',
    hex: 'admin.secrets.genHex',
    password: 'admin.secrets.genPassword'
  } as const satisfies Record<GenKind, string>;

  const ESCAPE_FREE_PASSWORD_ALPHABET =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~!*';
  const BYTES = 32;
  const PASSWORD_LENGTH = 40;

  let genKind = $state<GenKind>('base64');
  let generated = $state('');
  let genCopied = $state(false);

  function randomBytes(n: number): Uint8Array {
    return crypto.getRandomValues(new Uint8Array(n));
  }

  function generateValue(kind: GenKind): string {
    if (kind === 'password') {
      return [...randomBytes(PASSWORD_LENGTH)]
        .map((b) => ESCAPE_FREE_PASSWORD_ALPHABET[b % ESCAPE_FREE_PASSWORD_ALPHABET.length])
        .join('');
    }
    const bytes = randomBytes(BYTES);
    if (kind === 'hex') return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    return btoa(String.fromCharCode(...bytes));
  }

  function generate() {
    generated = generateValue(genKind);
    genCopied = false;
  }

  const copyGenerated = () => copyFlash(generated, (on: boolean) => (genCopied = on));
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.secrets.eyebrow')} description={t('admin.secrets.description')}>
    {t('admin.secrets.titlePre')}<em>{t('admin.secrets.titleEm')}</em>
  </PageHead>

  {#if bundle === null}
    <SkeletonStack rows={4} height="220px" columns={2} />
  {:else}
    <div class="grid">
      {#each services as service (service.id)}
        <ServiceCard
          {service}
          {canManage}
          onRotate={() => open('rotate', service)}
          onSet={() => open('set', service)}
          onRevoke={() => open('revoke', service)}
        />
      {/each}

      <Card class="gen-card">
        <CardHead title={t('admin.secrets.genTitle')}>
          {#snippet action()}
            <StatePill tone="free">{t('admin.secrets.genLocal')}</StatePill>
          {/snippet}
        </CardHead>
        <p class="note">{t('admin.secrets.genNote')}</p>
        <div class="kinds">
          {#each GEN_KINDS as kind (kind)}
            <Chip on={genKind === kind} onclick={() => (genKind = kind)}>
              {t(GEN_LABEL[kind])}
            </Chip>
          {/each}
        </div>
        <div class="gen-row">
          <Button variant="primary" onclick={generate}>{t('admin.secrets.generate')}</Button>
          {#if generated}
            <Input fill mono type="text" readonly value={generated} />
            <Button variant="ghost" onclick={copyGenerated}>
              {genCopied ? t('common.copied') : t('common.copy')}
            </Button>
          {/if}
        </div>
      </Card>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={dialog !== null}
  title={dialog ? t(dialog.title) : ''}
  confirmLabel={dialog ? t(dialog.cta) : t('common.done')}
  cancelLabel={t('common.cancel')}
  danger={dialog?.danger ?? false}
  {busy}
  onCancel={close}
  onConfirm={() => dialogForm?.requestSubmit()}
>
  {#if dialog && pendingService}
    <div class="fields">
      <p class="note">
        {t(dialog.body, {
          schema: pendingService.schema,
          project: pendingService.project,
          config: pendingService.config,
          service: pendingService.label
        })}
      </p>

      {#if dialog.needsUser}
        <Field label={t('admin.secrets.fieldDbUser')}>
          <Input
            fill mono
            type="text"
            autocomplete="off"
            placeholder={`${pendingService.expectedUserPrefix}_…`}
            bind:value={dbUser}
          />
        </Field>
      {/if}

      {#if dialog.needsPassword}
        <Field label={t('admin.secrets.fieldDbPass')}>
          <Input fill mono type="password" autocomplete="new-password" bind:value={dbPass} />
        </Field>
      {/if}

      <Field label={t('admin.secrets.fieldConfirm', { phrase })}>
        <Input fill mono type="text" autocomplete="off" bind:value={confirmText} />
      </Field>
      {#if !phraseMatches}
        <p class="note quiet">{t('admin.secrets.confirmHint')}</p>
      {/if}
    </div>
  {/if}
</ConfirmDialog>

{#if dialog && pendingService}
  <form
    method="POST"
    action={dialog.action}
    use:enhance={dialogSubmit}
    bind:this={dialogForm}
    hidden
  >
    <input type="hidden" name="service" value={pendingService.id} />
    <input type="hidden" name="confirm" value={confirmText} />
    <input type="hidden" name="db_user" value={dbUser} />
    <input type="hidden" name="db_pass" value={dbPass} />
  </form>
{/if}

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 16px;
  }

  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 12px;
  }
  .note.quiet {
    opacity: 0.75;
    margin: 0;
  }

  .kinds {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .gen-row {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  :global(.gen-card) {
    border-style: dashed;
  }

  .fields {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 12px 0 4px;
  }
</style>
