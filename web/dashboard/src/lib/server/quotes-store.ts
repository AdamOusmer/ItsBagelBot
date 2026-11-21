// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, RpcError } from '@bagel/kit/server/nats';
import {
  DISABLED_TRIGGERS_KEY,
  formatDisabledTriggers,
  MOD,
  moduleAllowedTriggers,
  moduleDef,
  parseDisabledTriggers
} from '@bagel/kit';
import { SUB } from './services';
import { patchModuleConfig, upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';

const QUOTES_MODULE = MOD.quotes;
const QUOTES_ALLOWED_TRIGGERS = moduleAllowedTriggers(moduleDef(QUOTES_MODULE)!);

const DEFAULT_PERM = 'mod';

const RPC_TIMEOUT_MS = 2000;

type BroadcasterId = string;
type QuoteVerb = 'list' | 'add' | 'edit' | 'remove';
type QuoteNumber = number;
type QuoteText = string;
type QuoteTimestamp = string;
type QuotePerm = string;
type TriggerList = string;

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
  disabledTriggers: string;
  quotes: QuoteView[];
}

interface QuoteState extends QuotePerms {
  enabled: boolean;
  disabledTriggers: string;
}

function quoteSubject(verb: QuoteVerb): string {
  return `${SUB.modules}.quote.${verb}`;
}

async function readModuleState(userId: BroadcasterId): Promise<QuoteState> {
  const { enabled, configs } = await readModuleBlob<Partial<QuotePerms & Record<typeof DISABLED_TRIGGERS_KEY, string>>>(
    userId,
    QUOTES_MODULE
  );
  return {
    enabled,
    addPerm: configs.addPerm || DEFAULT_PERM,
    editPerm: configs.editPerm || DEFAULT_PERM,
    disabledTriggers: String(configs[DISABLED_TRIGGERS_KEY] ?? '')
  };
}

async function listQuoteRows(userId: BroadcasterId): Promise<QuoteView[]> {
  const r = await rpc<{ quotes?: QuoteView[] }>(quoteSubject('list'), { user_id: userId }, RPC_TIMEOUT_MS);
  return r.quotes ?? [];
}

export async function readQuotes(userId: BroadcasterId): Promise<QuotesView> {
  const [state, quotes] = await Promise.all([readModuleState(userId), listQuoteRows(userId)]);
  return {
    enabled: state.enabled,
    addPerm: state.addPerm,
    editPerm: state.editPerm,
    disabledTriggers: state.disabledTriggers,
    quotes
  };
}

export interface QuoteDraft {
  text: string;
  addedBy: string;
  createdAt: string;
}

export async function addQuote(userId: BroadcasterId, draft: QuoteDraft): Promise<QuoteView> {
  const r = await rpc<{ quote?: QuoteView }>(
    quoteSubject('add'),
    { user_id: userId, text: draft.text, added_by: draft.addedBy, created_at: draft.createdAt },
    RPC_TIMEOUT_MS
  );
  if (!r.quote) throw new Error('quote add returned no row');
  return r.quote;
}

export async function editQuote(
  userId: BroadcasterId,
  num: QuoteNumber,
  text: QuoteText,
  createdAt: QuoteTimestamp
): Promise<QuoteView> {
  const r = await rpc<{ quote?: QuoteView; found?: boolean }>(
    quoteSubject('edit'),
    { user_id: userId, number: num, text, created_at: createdAt },
    RPC_TIMEOUT_MS
  );
  if (!r.found) throw new RpcError(`quote edit: #${num} does not exist`, 'not_found');
  if (!r.quote) throw new Error('quote edit returned no row');
  return r.quote;
}

export async function removeQuote(userId: BroadcasterId, num: QuoteNumber): Promise<boolean> {
  const r = await rpc<{ found?: boolean }>(quoteSubject('remove'), { user_id: userId, number: num }, RPC_TIMEOUT_MS);
  return !!r.found;
}

function configFor(state: Pick<QuoteState, 'addPerm' | 'editPerm' | 'disabledTriggers'>): Record<string, string> | undefined {
  const cfg: Record<string, string> = {};
  if (state.addPerm && state.addPerm !== DEFAULT_PERM) cfg.addPerm = state.addPerm;
  if (state.editPerm && state.editPerm !== DEFAULT_PERM) cfg.editPerm = state.editPerm;
  if (state.disabledTriggers) cfg[DISABLED_TRIGGERS_KEY] = state.disabledTriggers;
  return Object.keys(cfg).length ? cfg : undefined;
}

export async function setEnabled(userId: BroadcasterId, enabled: boolean): Promise<void> {
  await setModuleEnabled(userId, QUOTES_MODULE, enabled);
}

export async function setPerm(userId: BroadcasterId, kind: keyof QuotePerms, perm: QuotePerm): Promise<void> {
  const state = await readModuleState(userId);
  await upsertModule(userId, QUOTES_MODULE, state.enabled, configFor({ ...state, [kind]: perm }));
}

export async function setDisabledTriggers(userId: BroadcasterId, raw: TriggerList): Promise<string> {
  const normalized = formatDisabledTriggers(parseDisabledTriggers(raw), QUOTES_ALLOWED_TRIGGERS);
  await patchModuleConfig({ userId, name: QUOTES_MODULE, partial: { [DISABLED_TRIGGERS_KEY]: normalized } });
  return normalized;
}
