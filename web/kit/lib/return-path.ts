// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const RETURN_ORIGIN = 'https://return.invalid';

/** A normalized same-origin path for redirects, retaining its query and fragment. */
export function safeReturnPath(value: string | null | undefined): string | null {
  if (!value?.startsWith('/') || value.startsWith('//')) return null;
  // URL parsers discard ASCII whitespace/control characters and reinterpret
  // backslashes as slashes. Reject them before parsing rather than letting an
  // apparently local path become a protocol-relative redirect.
  if (/[\\\u0000-\u0020\u007f]/.test(value)) return null;
  try {
    const url = new URL(value, RETURN_ORIGIN);
    if (url.origin !== RETURN_ORIGIN || url.pathname.startsWith('//')) return null;
    return url.pathname + url.search + url.hash;
  } catch {
    return null;
  }
}
