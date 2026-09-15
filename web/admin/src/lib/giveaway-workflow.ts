export type PreviewSnapshot = { poolDigest?: string | null };

/** The freeze request must use the digest returned by the latest authoritative preview. */
export function freezePoolDigest(preview: PreviewSnapshot): string | null {
  const digest = preview.poolDigest?.trim() ?? '';
  return digest || null;
}
