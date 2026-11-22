// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export async function failureCode(response: Response, byStatus: Record<number, string>): Promise<string> {
  const body: unknown = await response.json().catch(() => null);
  const code = (body as { code?: unknown } | null)?.code;
  return typeof code === 'string' ? code : (byStatus[response.status] ?? 'failed');
}
