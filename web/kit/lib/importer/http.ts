// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export class CappedBodyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CappedBodyError';
  }
}

export async function readCappedText(res: Response, cap: number): Promise<string> {
  const reader = res.body?.getReader();
  if (!reader) return await res.text();
  return await decodeCappedChunks(reader, cap);
}

async function decodeCappedChunks(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  cap: number
): Promise<string> {
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > cap) {
      await reader.cancel();
      throw new CappedBodyError(`body exceeds ${cap} bytes`);
    }
    chunks.push(value);
  }
  return joinChunks(chunks, total);
}

function joinChunks(chunks: Uint8Array[], total: number): string {
  const merged = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) {
    merged.set(c, at);
    at += c.byteLength;
  }
  return new TextDecoder().decode(merged);
}

export function reasonOf(err: unknown): string {
  if (err instanceof Error) return err.name === 'AbortError' ? 'request timed out' : err.message;
  return String(err);
}
