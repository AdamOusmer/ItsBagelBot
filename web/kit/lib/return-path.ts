// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const RETURN_ORIGIN = 'https://return.invalid';

export function safeReturnPath(value: string | null | undefined): string | null {
  if (!value?.startsWith('/') || value.startsWith('//')) return null;
  // URL parsers drop control chars and read backslashes as slashes, enabling off-site redirects.
  if (/[\\\u0000-\u0020\u007f]/.test(value)) return null;
  try {
    const url = new URL(value, RETURN_ORIGIN);
    if (url.origin !== RETURN_ORIGIN || url.pathname.startsWith('//')) return null;
    return url.pathname + url.search + url.hash;
  } catch {
    return null;
  }
}
