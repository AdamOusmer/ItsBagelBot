// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { MOD } from '@bagel/kit';
import { SUB } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';

const QUOTES_MODULE = MOD.quotes;

const DEFAULT_PERM = 'mod';

const RPC_TIMEOUT_MS = 2000;

export interface QuoteView {
  number: number;
  text: string;
  added_by?: string;
  created_at: string;
}

export interface QuotePerms {
  addPerm: string;
  editPerm: string;
}

export interface QuotesView extends QuotePerms {
  enabled: boolean;
  quotes: QuoteView[];
}

function quoteSubject(verb: string): string {
  return `${SUB.modules}.quote.${verb}`;
}

async function readModuleState(userId: string): Promise<{ enabled: boolean } & QuotePerms> {
  const { enabled, configs } = await readModuleBlob<Partial<QuotePerms>>(userId, QUOTES_MODULE);
  return {
    enabled,
    addPerm: configs.addPerm || DEFAULT_PERM,
    editPerm: configs.editPerm || DEFAULT_PERM
  };
}

async function listQuoteRows(userId: string): Promise<QuoteView[]> {
  const r = await rpc<{ quotes?: QuoteView[] }>(quoteSubject('list'), { user_id: userId }, RPC_TIMEOUT_MS);
  return r.quotes ?? [];
}

export async function readQuotes(userId: string): Promise<QuotesView> {
  const [state, quotes] = await Promise.all([readModuleState(userId), listQuoteRows(userId)]);
  return { enabled: state.enabled, addPerm: state.addPerm, editPerm: state.editPerm, quotes };
}

export interface QuoteDraft {
  text: string;
  addedBy: string;
  createdAt: string;
}

export async function addQuote(userId: string, draft: QuoteDraft): Promise<QuoteView> {
  const r = await rpc<{ quote?: QuoteView }>(
    quoteSubject('add'),
    { user_id: userId, text: draft.text, added_by: draft.addedBy, created_at: draft.createdAt },
    RPC_TIMEOUT_MS
  );
  if (!r.quote) throw new Error('quote add returned no row');
  return r.quote;
}

export async function editQuote(userId: string, num: number, text: string, createdAt: string): Promise<QuoteView> {
  const r = await rpc<{ quote?: QuoteView; found?: boolean }>(
    quoteSubject('edit'),
    { user_id: userId, number: num, text, created_at: createdAt },
    RPC_TIMEOUT_MS
  );
  if (!r.found || !r.quote) throw new Error(`quote edit: #${num} does not exist`);
  return r.quote;
}

export async function removeQuote(userId: string, num: number): Promise<boolean> {
  const r = await rpc<{ found?: boolean }>(quoteSubject('remove'), { user_id: userId, number: num }, RPC_TIMEOUT_MS);
  return !!r.found;
}

function configFor(perms: QuotePerms): Record<string, string> | undefined {
  const cfg: Record<string, string> = {};
  if (perms.addPerm && perms.addPerm !== DEFAULT_PERM) cfg.addPerm = perms.addPerm;
  if (perms.editPerm && perms.editPerm !== DEFAULT_PERM) cfg.editPerm = perms.editPerm;
  return Object.keys(cfg).length ? cfg : undefined;
}

export async function setEnabled(userId: string, enabled: boolean): Promise<void> {
  await setModuleEnabled(userId, QUOTES_MODULE, enabled);
}

export async function setPerm(userId: string, kind: keyof QuotePerms, perm: string): Promise<void> {
  const state = await readModuleState(userId);
  await upsertModule(userId, QUOTES_MODULE, state.enabled, configFor({ ...state, [kind]: perm }));
}
