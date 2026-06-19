<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon } from '@bagel/shared';

  type Service = {
    id: string;
    label: string;
    project: string;
    config: string;
    schema: string;
    expectedUserPrefix: string;
    dbUser: string;
    autoMigrate: string;
    canReadDoppler: boolean;
  };

  let { data, form } = $props();
  let selectedId = $state('');
  let setUser = $state('');
  let revokeUser = $state('');

  const result = $derived(form as { ok?: boolean; notice?: string; error?: string } | undefined);
  const selected = $derived<Service>(
    (data.services as Service[]).find((service) => service.id === selectedId) ?? data.services[0]
  );

  function selectService(service: Service) {
    selectedId = service.id;
    setUser = `${service.expectedUserPrefix}_${Date.now().toString(36).slice(-5)}`;
    revokeUser = '';
  }

  function refresh() {
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update({ invalidateAll: true });
    };
  }
</script>

<section class="page-head">
  <div>
    <p class="eyebrow">Access</p>
    <h1>DB credentials</h1>
    <p class="lede">Schema-local runtime identities for the data services.</p>
  </div>
</section>

{#if result?.notice || result?.error}
  <div class:ok={result?.ok} class:bad={!result?.ok} class="notice">
    {result.notice ?? result.error}
  </div>
{/if}

<section class="grid">
  <div class="panel services">
    <div class="panel-head">
      <h2>Services</h2>
    </div>
    <div class="service-list">
      {#each data.services as svc}
        <button class:active={selected.id === svc.id} type="button" onclick={() => selectService(svc)}>
          <span>
            <b>{svc.label}</b>
            <em>{svc.schema}</em>
          </span>
          <Icon name={svc.canReadDoppler ? 'check' : 'blocked'} size={16} />
        </button>
      {/each}
    </div>
  </div>

  <div class="panel detail">
    <div class="panel-head">
      <h2>{selected.label}</h2>
      <span class="badge">{selected.project}/{selected.config}</span>
    </div>

    <div class="facts">
      <div><span>Schema</span><b>{selected.schema}</b></div>
      <div><span>Doppler user</span><b>{selected.dbUser || 'unavailable'}</b></div>
      <div><span>Auto migrate</span><b>{selected.autoMigrate || 'unavailable'}</b></div>
      <div><span>Expected prefix</span><b>{selected.expectedUserPrefix}</b></div>
    </div>

    <div class="actions-grid">


      <form method="POST" action="?/set" use:enhance={refresh} autocomplete="off" class="action-box">
        <input type="hidden" name="service" value={selected.id} />
        <div class="box-title">
          <Icon name="edit" size={17} />
          <h3>Set</h3>
        </div>
        <input name="db_user" bind:value={setUser} placeholder={`${selected.expectedUserPrefix}_next`} autocomplete="off" />
        <input name="db_pass" type="password" placeholder="new password" autocomplete="new-password" />
        <input name="confirm" placeholder={`set ${selected.id}`} autocomplete="off" />
        <button class="btn ghost" type="submit">
          <Icon name="check" size={14} /> Set credential
        </button>
      </form>

      <form method="POST" action="?/revoke" use:enhance={refresh} autocomplete="off" class="action-box danger">
        <input type="hidden" name="service" value={selected.id} />
        <div class="box-title">
          <Icon name="trash" size={17} />
          <h3>Revoke</h3>
        </div>
        <input name="db_user" bind:value={revokeUser} placeholder="old runtime user" autocomplete="off" />
        <input name="confirm" placeholder="revoke username" autocomplete="off" />
        <button class="btn ghost danger-btn" type="submit">
          <Icon name="ban" size={14} /> Revoke user
        </button>
      </form>
    </div>
  </div>
</section>

<style>
  .page-head {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: flex-end;
    flex-wrap: wrap;
    margin-bottom: 16px;
  }
  .eyebrow {
    margin: 0 0 4px;
    color: var(--bb-muted);
    text-transform: uppercase;
    font-size: 0.74rem;
    letter-spacing: 0.08em;
  }
  h1 {
    margin: 0;
    font-size: 1.8rem;
  }
  .lede {
    margin: 6px 0 0;
    color: var(--bb-muted);
  }
  .notice {
    border: 1px solid var(--bb-border);
    background: rgba(255,255,255,.06);
    border-radius: 8px;
    padding: 10px 12px;
    margin-bottom: 14px;
  }
  .notice.ok {
    border-color: rgba(82,183,136,.45);
  }
  .notice.bad {
    border-color: rgba(255,100,100,.45);
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .panel {
    border: 1px solid var(--bb-border);
    background: rgba(255,255,255,.045);
    border-radius: 8px;
    padding: 14px;
  }
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  h2, h3 {
    margin: 0;
  }
  .badge {
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 4px 8px;
    color: var(--bb-muted);
    font-size: .82rem;
  }
  .service-list {
    display: grid;
    gap: 8px;
  }
  .service-list button {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 10px;
    width: 100%;
    text-align: left;
    color: inherit;
    border: 1px solid var(--bb-border);
    background: rgba(255,255,255,.035);
    border-radius: 8px;
    padding: 10px;
    cursor: pointer;
  }
  .service-list button.active {
    border-color: var(--bb-border-strong);
    background: rgba(255,255,255,.08);
  }
  .service-list span,
  .facts div,
  .box-title {
    display: grid;
    gap: 3px;
  }
  .service-list em,
  .facts span {
    color: var(--bb-muted);
    font-size: .82rem;
    font-style: normal;
  }
  .facts {
    display: grid;
    grid-template-columns: 1fr;
    gap: 10px;
    margin-bottom: 14px;
  }
  .facts div {
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    padding: 10px;
    background: rgba(0,0,0,.12);
  }
  .facts b {
    overflow-wrap: anywhere;
  }
  .actions-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .action-box {
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    padding: 12px;
    display: grid;
    gap: 10px;
    align-content: start;
    background: rgba(0,0,0,.12);
  }
  .action-box.danger {
    border-color: rgba(255,100,100,.28);
  }
  .box-title {
    grid-template-columns: auto 1fr;
    align-items: center;
  }
  input {
    width: 100%;
    min-height: 38px;
    border-radius: 8px;
    border: 1px solid var(--bb-border);
    background: rgba(0,0,0,.22);
    color: inherit;
    padding: 0 10px;
    outline: none;
  }
  input:focus {
    border-color: var(--bb-border-strong);
  }
  .danger-btn {
    border-color: rgba(255,100,100,.4);
  }
  @media (min-width: 981px) {
    .grid {
      grid-template-columns: minmax(230px, 300px) 1fr;
    }
    .facts {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .actions-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
