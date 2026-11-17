// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { buildDonations, type DonationsReply } from './donations-view';
import type { DataTable } from './analytics-page';

const cells = (table: DataTable) => table.rows.map((row) => ['text' in row.name ? row.name.text : row.name.key, ...row.cells.map((c) => c.text)]);

const reply: DonationsReply = {
  totals: [
    { currency: 'USD', gross_cents: 123_450, payments: 3, recurring_cents: 30_000, recurring_payments: 1, first_subscriptions: 1, previous_gross_cents: 100_000, previous_payments: 2 },
    { currency: 'EUR', gross_cents: 500, payments: 1, recurring_cents: 0, recurring_payments: 0, first_subscriptions: 2, previous_gross_cents: 0, previous_payments: 0 }
  ],
  months: [{ month: '2026-11', currency: 'JPY', gross_cents: 50_000, payments: 1 }],
  kinds: [{ kind: 'donation', payments: 3 }, { kind: 'subscription', payments: 1 }],
  last_payment_at: '2026-11-15T23:10:00Z'
};

describe('donations', () => {
  it('formats money per currency with the previous window change', () => {
    expect(cells(buildDonations(reply).totals)).toEqual([
      ['USD', '$1,234.50', '3', '$411.50', '$300.00', '24%', '$1,000.00', '+23%'],
      ['EUR', '€5.00', '1', '€5.00', '€0.00', '0%', '€0.00', '-']
    ]);
  });

  it('keeps every Ko-fi type and month row', () => {
    const view = buildDonations(reply);
    expect({
      months: cells(view.months),
      kinds: view.kinds.map((bar) => [bar.id, bar.valueText, bar.secondary?.params?.share]),
      facts: view.totalFacts
    }).toEqual({
      months: [['2026-11', 'JPY', '¥500', '1']],
      kinds: [['donation', '3', '75%'], ['subscription', '1', '25%'], ['commission', '0', '0%'], ['shop_order', '0', '0%']],
      facts: [{ key: 'newSubscribers', value: '3' }, { key: 'lastDonation', value: 'Nov 15, 2026' }]
    });
  });

  it('renders an empty ledger without rows', () => {
    const view = buildDonations({});
    expect({ totals: view.totals.rows, months: view.months.rows, last: view.totalFacts[1].value }).toEqual({ totals: [], months: [], last: '-' });
  });
});
