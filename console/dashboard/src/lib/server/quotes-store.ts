// Quotes store: the channel quote book behind the quotes module.
//
// Unlike timers (whose list lives inside the module blob), quotes are
// DB-backed rows owned by the modules service and reached through its quote
// verbs (bagel.rpc.modules.quote.*). The module blob still holds the two
// settings — the enable flag and addPerm (who may save from chat) — so this
// store reads/writes those through the same listModules/upsertModule path
// every other module uses, and reads/mutates the rows through the quote RPC.
import { rpc } from '@bagel/shared/server/nats';
import { SUB } from './services';
import { listModules, upsertModule } from './commands-store';

const QUOTES_MODULE = 'quotes';

// Default save permission when the blob has no addPerm set; mirrors the sesame
// module's quoteAddRole default (empty -> moderator).
const DEFAULT_ADD_PERM = 'mod';

const RPC_TIMEOUT_MS = 2000;

export interface QuoteView {
  number: number;
  text: string;
  added_by?: string;
  created_at: string;
}

export interface QuotesView {
  enabled: boolean;
  addPerm: string;
  quotes: QuoteView[];
}

function quoteSubject(verb: string): string {
  return `${SUB.modules}.quote.${verb}`;
}

// readModuleState pulls the enable flag and addPerm out of the quotes module
// blob. A missing row means the module has never been configured (disabled,
// default perm).
async function readModuleState(userId: string): Promise<{ enabled: boolean; addPerm: string }> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === QUOTES_MODULE);
  const configs = (row?.configs ?? {}) as { addPerm?: string };
  return {
    enabled: row ? row.is_enabled : false,
    addPerm: configs.addPerm || DEFAULT_ADD_PERM
  };
}

// listQuoteRows fetches the whole book from the modules service.
async function listQuoteRows(userId: string): Promise<QuoteView[]> {
  const r = await rpc<{ quotes?: QuoteView[] }>(quoteSubject('list'), { user_id: userId }, RPC_TIMEOUT_MS);
  return r.quotes ?? [];
}

// readQuotes assembles the page view: module settings plus the book.
export async function readQuotes(userId: string): Promise<QuotesView> {
  const [state, quotes] = await Promise.all([readModuleState(userId), listQuoteRows(userId)]);
  return { enabled: state.enabled, addPerm: state.addPerm, quotes };
}

// QuoteDraft is a new quote's content: the body plus the login stamped as its
// audit added_by. Bundled so addQuote takes one domain value, not a row of
// bare strings.
export interface QuoteDraft {
  text: string;
  addedBy: string;
  createdAt: string;
}

// addQuote saves a new quote and returns it with its assigned number. A thrown
// RpcError (validation, e.g. too long/empty) propagates so the action reports
// the real reason.
export async function addQuote(userId: string, draft: QuoteDraft): Promise<QuoteView> {
  const r = await rpc<{ quote?: QuoteView }>(
    quoteSubject('add'),
    { user_id: userId, text: draft.text, added_by: draft.addedBy, created_at: draft.createdAt },
    RPC_TIMEOUT_MS
  );
  if (!r.quote) throw new Error('quote add returned no row');
  return r.quote;
}

// removeQuote deletes one quote by number; false means it did not exist.
export async function removeQuote(userId: string, num: number): Promise<boolean> {
  const r = await rpc<{ found?: boolean }>(quoteSubject('remove'), { user_id: userId, number: num }, RPC_TIMEOUT_MS);
  return !!r.found;
}

// configFor builds the module blob: store addPerm only when it differs from the
// moderator default, so an unconfigured channel keeps an empty blob (matching
// the sesame default resolution).
function configFor(addPerm: string): Record<string, string> | undefined {
  return addPerm && addPerm !== DEFAULT_ADD_PERM ? { addPerm } : undefined;
}

// setEnabled flips the module on/off, preserving addPerm.
export async function setEnabled(userId: string, enabled: boolean): Promise<void> {
  const state = await readModuleState(userId);
  await upsertModule(userId, QUOTES_MODULE, enabled, configFor(state.addPerm));
}

// setAddPerm changes who may save a quote from chat, preserving the enable flag.
export async function setAddPerm(userId: string, addPerm: string): Promise<void> {
  const state = await readModuleState(userId);
  await upsertModule(userId, QUOTES_MODULE, state.enabled, configFor(addPerm));
}
