<script lang="ts">
  // Inline module config editor — the module twin of CommandEditor. Renders the
  // ?/save form for one module: the module-level enable toggle plus each catalog
  // field (text / textarea / number / per-alert toggle). The working copy
  // (enabled + config) is bound back to the page so its optimistic save can read
  // the submitted values. Field labels, spacing, and controls match
  // CommandEditor so the docked inspector reads as one surface.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, getI18n, type ModuleState } from '@bagel/shared';

  let {
    module: mod,
    enabled = $bindable<boolean>(),
    config = $bindable<Record<string, string>>(),
    busy = false,
    onCancel,
    onSubmit
  }: {
    module: ModuleState;
    enabled: boolean;
    config: Record<string, string>;
    busy?: boolean;
    onCancel: () => void;
    onSubmit: SubmitFunction;
  } = $props();

  const { t } = getI18n();

  // A per-alert toggle is on unless explicitly stored "off" (matches the sesame
  // alertOn semantics: empty/absent means default-on).
  const fieldOn = (key: string) => config[key] !== 'off';
  function toggleField(key: string) {
    config = { ...config, [key]: fieldOn(key) ? 'off' : 'on' };
  }
</script>

<form method="POST" action="?/save" class="editor" use:enhance={onSubmit}>
  <input type="hidden" name="name" value={mod.def.id} />

  <!-- Module enable toggle: horizontal row with a hairline beneath it. -->
  <div class="toggle-row">
    <div class="tr-text">
      <span class="tr-label">{t('modules.enabled')}</span>
      <span class="tr-help">{t('modules.enabledHelp')}</span>
    </div>
    <input type="hidden" name="is_enabled" value={enabled ? 'on' : ''} />
    <button
      class="toggle {enabled ? 'on' : ''}"
      type="button"
      aria-label={t('modules.toggleAria', { label: mod.def.label })}
      onclick={() => (enabled = !enabled)}
    ></button>
  </div>

  {#each mod.def.fields as field (field.key)}
    {#if field.type === 'toggle'}
      <div class="toggle-row sub">
        <div class="tr-text">
          <span class="tr-label sm">{field.label}</span>
          {#if field.help}<span class="tr-help">{field.help}</span>{/if}
        </div>
        <input type="hidden" name={`cfg.${field.key}`} value={fieldOn(field.key) ? 'on' : 'off'} />
        <button
          class="toggle {fieldOn(field.key) ? 'on' : ''}"
          type="button"
          aria-label={t('modules.toggleAria', { label: field.label })}
          onclick={() => toggleField(field.key)}
        ></button>
      </div>
    {:else}
      <label class="field">
        <span>{field.label}</span>
        {#if field.type === 'textarea'}
          <textarea
            class="resp-area"
            name={`cfg.${field.key}`}
            rows="3"
            placeholder={field.placeholder ?? ''}
            bind:value={config[field.key]}
          ></textarea>
        {:else}
          <input
            class="search"
            type={field.type === 'number' ? 'number' : 'text'}
            name={`cfg.${field.key}`}
            placeholder={field.placeholder ?? ''}
            bind:value={config[field.key]}
          />
        {/if}
        {#if field.help}<small>{field.help}</small>{/if}
      </label>
    {/if}
  {/each}

  <div class="actions">
    <button type="button" class="btn ghost" onclick={onCancel} disabled={busy}>{t('common.cancel')}</button>
    <button type="submit" class="btn primary" disabled={busy}>
      <Icon name="check" size={14} />
      {busy ? t('modules.loading') : t('modules.saveChanges')}
    </button>
  </div>
</form>

<style>
  .editor { padding: 4px 2px 2px; }

  /* Enable toggle row (module-level, and the per-alert sub-toggles). */
  .toggle-row {
    display: flex;
    align-items: center;
    gap: 14px;
    padding-bottom: 14px;
    margin-bottom: 14px;
    border-bottom: 1px solid var(--rule, var(--glass-border));
  }
  .toggle-row.sub {
    padding: 12px 0;
    margin-bottom: 0;
    border-bottom: 1px solid var(--rule, var(--glass-border));
  }
  .tr-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .tr-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 14px; color: var(--bb-white); }
  .tr-label.sm { font-size: 13px; }
  .tr-help { font-size: 12px; color: var(--bb-muted); line-height: 1.4; }
  .toggle-row .toggle { margin-left: auto; }

  /* Text/number/textarea fields: copied from CommandEditor. */
  .field { display: flex; flex-direction: column; gap: 6px; margin: 14px 0; }
  .field > span {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    letter-spacing: 0.01em;
  }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field :global(.search) { width: 100%; box-sizing: border-box; }

  .resp-area {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 84px;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.6;
  }
  .resp-area:focus {
    outline: none;
    border-color: rgba(82, 183, 136, 0.5);
    box-shadow: 0 0 0 3px rgba(82, 183, 136, 0.12);
  }

  .actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 16px; }

  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
