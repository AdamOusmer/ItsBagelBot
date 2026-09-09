// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// CSV export, once. Two pages offer it (the user directory and the audit
// trail) and both had written their own `csvEscape` plus their own
// Blob/anchor/revokeObjectURL dance. The two escapes had already drifted: one
// tested for `[",\n]`, the other only for a comma, so an audit `detail`
// containing a quote shifted every column after it.

/**
 * RFC 4180 quoting: a field containing a quote, comma or newline is wrapped and
 * its own quotes doubled. Spelled out rather than joined naively because both
 * exports carry operator-supplied free text (a creator code, an audit detail).
 */
export function csvCell(value: string): string {
  return /[",\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

/** A header row plus quoted body rows, newline-joined. */
export function csvDocument(header: string, rows: readonly (readonly string[])[]): string {
  return [header, ...rows.map((cells) => cells.map(csvCell).join(','))].join('\n');
}

/**
 * Hand `csv` to the browser as a download named `filename`.
 *
 * The object URL is revoked immediately after the synthetic click, which is
 * safe: the click starts the download synchronously and the browser has already
 * taken its own reference to the blob by the time this returns. Leaving it
 * unrevoked pins the whole export in memory for the life of the document, which
 * on the users page is the entire loaded directory.
 */
export function downloadCsv(filename: string, csv: string): void {
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
