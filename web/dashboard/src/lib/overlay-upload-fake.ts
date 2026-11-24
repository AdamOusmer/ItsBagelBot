// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Call = { url: string; method: string; body: unknown };

export function fakeUpload(final: Response, ticket = { ticket: 'tkt-0123456789abcdef', max_size: 100, expires: 1, part_size: 3, parts: 2 }) {
  const calls: Call[] = [];
  const send = (async (url: string, init: RequestInit) => {
    calls.push({ url, method: String(init.method), body: init.body });
    if (init.method === 'POST') return Response.json(ticket);
    return url.includes('done=1') ? final : Response.json({ part: 1 });
  }) as unknown as typeof fetch;
  return { calls, send };
}
