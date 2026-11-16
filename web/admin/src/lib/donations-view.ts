// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Bar, DataTable, Fact, TableRow } from './analytics-page';
import { dateLabel, fmt, pct, relativeChange } from './analytics-view';

export type DonationTotalWire = {
  currency: string;
  gross_cents: number;
  payments: number;
  recurring_cents: number;
  recurring_payments: number;
  first_subscriptions: number;
  previous_gross_cents: number;
  previous_payments: number;
};
export type DonationMonthWire = { month: string; currency: string; gross_cents: number; payments: number };
export type DonationKindWire = { kind: string; payments: number };
export type DonationsReply = {
  totals?: DonationTotalWire[] | null;
  months?: DonationMonthWire[] | null;
  kinds?: DonationKindWire[] | null;
  last_payment_at?: string | null;
};

export type DonationsView = { totals: DataTable; totalFacts: Fact[]; months: DataTable; kinds: Bar[] };

export const DONATION_KINDS = ['donation', 'subscription', 'commission', 'shop_order'] as const;
const TOTAL_COLUMNS = ['gross', 'payments', 'average', 'recurring', 'recurringShare', 'previousGross', 'grossChange'];
const MONTH_COLUMNS = ['currency', 'gross', 'payments'];

export function money(cents: number, currency: string): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(cents / 100);
}

function line(id: string, name: string, texts: string[]): TableRow {
  return { id, name: { text: name }, cells: texts.map((text) => ({ text })) };
}

function totalRow(total: DonationTotalWire): TableRow {
  const cash = (cents: number) => money(cents, total.currency);
  const average = total.payments ? cash(Math.round(total.gross_cents / total.payments)) : '-';
  return line(total.currency, total.currency, [
    cash(total.gross_cents),
    fmt(total.payments),
    average,
    cash(total.recurring_cents),
    pct(total.recurring_cents, total.gross_cents),
    cash(total.previous_gross_cents),
    relativeChange(total.gross_cents, total.previous_gross_cents)?.text ?? '-'
  ]);
}

function monthRow(month: DonationMonthWire): TableRow {
  return line(`${month.month}:${month.currency}`, month.month, [month.currency, money(month.gross_cents, month.currency), fmt(month.payments)]);
}

function kindBars(kinds: DonationKindWire[]): Bar[] {
  const counts = new Map(kinds.map((k) => [k.kind, k.payments]));
  const all = kinds.reduce((n, k) => n + k.payments, 0);
  return DONATION_KINDS.map((kind) => {
    const value = counts.get(kind) ?? 0;
    return {
      id: kind,
      name: { key: `admin.analytics.dim.donation.${kind}` },
      value,
      valueText: fmt(value),
      secondary: { key: 'admin.analytics.share.ofTotal', params: { share: pct(value, all) } }
    };
  });
}

export function buildDonations(reply: DonationsReply): DonationsView {
  const totals = reply.totals ?? [];
  const firstSubscriptions = totals.reduce((n, t) => n + t.first_subscriptions, 0);
  const last = reply.last_payment_at ? dateLabel(reply.last_payment_at.slice(0, 10)) : '-';
  return {
    totals: { columns: TOTAL_COLUMNS, rows: totals.map(totalRow) },
    totalFacts: [
      { key: 'newSubscribers', value: fmt(firstSubscriptions) },
      { key: 'lastDonation', value: last }
    ],
    months: { columns: MONTH_COLUMNS, rows: (reply.months ?? []).map(monthRow) },
    kinds: kindBars(reply.kinds ?? [])
  };
}
