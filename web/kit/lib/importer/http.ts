// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Transport posture shared by every fetch-backed import source: read an
// upstream reply into memory, but never more of it than a cap allows.
//
// This used to live inside nightbot/fetch.ts alone. Fossabot's fetch layer
// needs the identical bound (a public, unauthenticated endpoint answering into
// the dashboard pod), and two copies of a streaming read is exactly the kind of
// helper that drifts on one side only, so the reader moved here and both fetch
// layers call it. The prose each source wraps the failure in stays its own: a
// broadcaster reads "no Fossabot channel named …", not a byte count.

// CappedBodyError marks the one failure this module owns: the upstream body
// grew past the cap and the read was cancelled. Callers translate it into
// their own source-prefixed error type; every other read failure propagates
// untouched, since only the caller knows which endpoint it was reading.
export class CappedBodyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CappedBodyError';
  }
}

// readCappedText decodes the response body as UTF-8 text, refusing to buffer
// more than cap bytes: a hostile or broken server streaming forever cannot
// balloon the pod's memory. A body-less response falls back to res.text(),
// which has nothing to stream.
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

// reasonOf renders a caught value as the fragment a fetch error message ends
// with: an AbortController firing reads as a timeout (its own name is the only
// signal fetch gives), everything else as its message.
export function reasonOf(err: unknown): string {
  if (err instanceof Error) return err.name === 'AbortError' ? 'request timed out' : err.message;
  return String(err);
}
