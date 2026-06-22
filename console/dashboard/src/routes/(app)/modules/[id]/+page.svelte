<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, Card, PageHead } from '@bagel/shared';
  let { data } = $props();

  const def = $derived(data.state.def);
  // svelte-ignore state_referenced_locally
  let enabled = $state(data.state.enabled);
  // svelte-ignore state_referenced_locally
  let config = $state<Record<string, string>>({ ...data.state.config });

  // Re-seed the form when navigating to a different module (component reuse).
  // svelte-ignore state_referenced_locally
  let seed = data.state;
  $effect(() => {
    if (data.state !== seed) {
      seed = data.state;
      enabled = data.state.enabled;
      config = { ...data.state.config };
    }
  });

  let toast = $state<{ kind: 'ok' | 'err'; text: string } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | undefined;
  function flash(kind: 'ok' | 'err', text: string) {
    toast = { kind, text };
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (toast = null), 3000);
  }
</script>

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> All modules</a>
  <PageHead eyebrow="Module" description={def.description}>{def.label}</PageHead>

  <Card style="padding:0;max-width:640px">
    <form
      method="POST"
      action="?/save"
      use:enhance={() => async ({ result }) => {
        if (result.type === 'success' && result.data?.ok) flash('ok', `${def.label} saved.`);
        else if (result.type === 'failure') flash('err', String(result.data?.error ?? 'Save failed.'));
      }}
    >
      <div class="row toprow">
        <div class="row-text">
          <span class="row-label">Enabled</span>
          <span class="row-help">Turn this module on or off for your channel.</span>
        </div>
        <input type="hidden" name="is_enabled" value={enabled ? 'on' : ''} />
        <button
          class="toggle {enabled ? 'on' : ''}"
          type="button"
          aria-label="Toggle {def.label}"
          onclick={() => (enabled = !enabled)}
        ></button>
      </div>

      {#each def.fields as field (field.key)}
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
      {/each}

      <div class="actions">
        <a class="btn ghost" href="/modules">Cancel</a>
        <button class="btn primary" type="submit"><Icon name="check" size={14} /> Save changes</button>
      </div>
    </form>
  </Card>
</section>

{#if toast}
  <div class="toast {toast.kind}" role="status">
    <Icon name={toast.kind === 'ok' ? 'check' : 'ban'} size={15} />
    <span>{toast.text}</span>
  </div>
{/if}

<style>
  .back {
    display: inline-flex; align-items: center; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
    text-decoration: none; margin-bottom: 10px;
  }
  .back:hover { color: var(--bb-white); }

  .row { display: flex; align-items: center; gap: 14px; padding: 18px 20px; }
  .toprow { border-bottom: 1px solid var(--glass-border); }
  .row-text { display: flex; flex-direction: column; gap: 3px; }
  .row-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .row-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .row .toggle { margin-left: auto; }

  .field { display: flex; flex-direction: column; gap: 6px; padding: 16px 20px 0; }
  .field > span { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .field small { color: var(--bb-muted); opacity: 0.7; font-size: 11px; }
  .field .search { width: 100%; box-sizing: border-box; }
  .resp-area {
    width: 100%; box-sizing: border-box; resize: vertical; min-height: 84px;
    padding: 12px 14px; background: rgba(255, 255, 255, 0.03);
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-md, 10px);
    color: var(--bb-white); font-family: var(--bb-font-body); font-size: 13.5px; line-height: 1.6;
  }
  .resp-area:focus { outline: none; border-color: rgba(82, 183, 136, 0.5); box-shadow: 0 0 0 3px rgba(82, 183, 136, 0.12); }

  .actions { display: flex; justify-content: flex-end; gap: 10px; padding: 20px; }

  .toast {
    position: fixed; right: 20px; bottom: 20px; z-index: 300;
    display: flex; align-items: center; gap: 9px; padding: 12px 16px;
    border-radius: var(--bb-radius-md, 10px); background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong); box-shadow: 0 14px 40px rgba(0, 0, 0, 0.5);
    font-family: var(--bb-font-body); font-size: 13.5px; color: var(--bb-white);
  }
  .toast.ok { border-color: rgba(82, 183, 136, 0.45); color: var(--bb-green-glow); }
  .toast.err { border-color: rgba(176, 90, 70, 0.45); color: #cf8a78; }

  @media (max-width: 480px) {
    .actions { flex-direction: column-reverse; }
    .actions .btn { width: 100%; justify-content: center; min-height: 44px; }
  }
</style>
