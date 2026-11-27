// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SUB } from '#lib/server/services/subjects';
import type { QuoteView } from '#lib/server/quotes-store';
import { demoQuotes } from '../fixtures';
import type { Payload, Routes } from './types';

const quotes: QuoteView[] = demoQuotes();

function quoteRow(payload: Payload, number: number, addedBy: string): QuoteView {
  return { number, text: String(payload.text ?? ''), added_by: addedBy, created_at: String(payload.created_at ?? '') };
}

function addQuote(payload: Payload) {
  const quote = quoteRow(payload, Math.max(0, ...quotes.map((q) => q.number)) + 1, String(payload.added_by ?? 'dashboard'));
  quotes.push(quote);
  return { quote };
}

function editQuote(payload: Payload) {
  const at = quotes.findIndex((q) => q.number === payload.number);
  if (at < 0) return { found: false };
  quotes[at] = quoteRow(payload, quotes[at].number, quotes[at].added_by ?? 'dashboard');
  return { found: true, quote: quotes[at] };
}

function removeQuote(payload: Payload) {
  const at = quotes.findIndex((q) => q.number === payload.number);
  if (at >= 0) quotes.splice(at, 1);
  return { found: at >= 0 };
}

const quote = (verb: string) => `${SUB.modules}.quote.${verb}`;

export const quoteRoutes: Routes = {
  [quote('list')]: () => ({ quotes }),
  [quote('add')]: addQuote,
  [quote('edit')]: editQuote,
  [quote('remove')]: removeQuote
};
