// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type UploadBegin = { name?: string; dry_run?: boolean };
type UploadTicket = { ticket: string; part_size: number; parts: number };

const JSON_HEADERS = { 'content-type': 'application/json' };

export async function sendParts(endpoint: string, file: File, begin: UploadBegin, send: typeof fetch): Promise<Response> {
  const opened = await send(endpoint, { method: 'POST', headers: JSON_HEADERS, body: JSON.stringify({ declared_size: file.size, ...begin }) });
  if (!opened.ok) return opened;
  const { ticket, part_size, parts } = (await opened.json()) as UploadTicket;
  const target = `${endpoint}?ticket=${encodeURIComponent(ticket)}`;
  for (let part = 1; part <= parts; part++) {
    const start = (part - 1) * part_size;
    const sent = await send(`${target}&part=${part}`, { method: 'PUT', body: file.slice(start, start + part_size) });
    if (!sent.ok) return sent;
  }
  return send(`${target}&done=1${begin.dry_run ? '&dry_run=1' : ''}`, { method: 'PUT' });
}
