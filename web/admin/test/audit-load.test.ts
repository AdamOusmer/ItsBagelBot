// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { afterAll, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import type { AuditPage } from '../src/lib/server/services';
import type { AuditPageResult } from '../src/lib/components/audit/audit-page.server';

stubSvelteKit();

function deferredAudit() {
  let resolve!: (page: AuditPage) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<AuditPage>((success, failure) => {
    resolve = success;
    reject = failure;
  });
  return { promise, resolve, reject };
}

let reply = deferredAudit();
const services = { ...await import('../src/lib/server/services') };
const auditPage = mock((_page: number, _search: string) => reply.promise);
mock.module('#lib/server/services', () => ({ ...services, auditPage }));
afterAll(() => mock.module('#lib/server/services', () => services));

const { load } = await import('../src/routes/(admin)/audit/+page.server');

type StreamCase = { label: string; query: string; page: number; search: string; error?: string };
const cases: StreamCase[] = [
  { label: 'initial page', query: '', page: 1, search: '' },
  { label: 'paged search', query: '?page=3&q=%20operator%20', page: 3, search: 'operator' },
  { label: 'capped page during outage', query: '?page=99', page: 25, search: '', error: 'unavailable' }
];

test.each(cases)('streams audit results for $label', async ({ query, page, search, error }: StreamCase) => {
  reply = deferredAudit();
  auditPage.mockClear();
  const data = await load({
    parent: async () => ({ role: 'owner' }),
    url: new URL('https://admin.example/audit' + query)
  } as never) as { firstPage: Promise<AuditPageResult>; page: number; search: string };

  expect({ requested: auditPage.mock.calls, page: data.page, search: data.search, streamed: data.firstPage instanceof Promise })
    .toEqual({ requested: [[page, search]], page, search, streamed: true });

  const payload: AuditPage = { entries: [], page, page_size: 15, max_pages: 25, has_more: !error };
  if (error) reply.reject(new Error(error));
  else reply.resolve(payload);
  expect(await data.firstPage).toEqual(error ? { ...payload, error } : payload);
});
