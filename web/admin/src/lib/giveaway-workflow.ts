export type PreviewSnapshot = { poolDigest?: string | null };

export function freezePoolDigest(preview: PreviewSnapshot): string | null {
  const digest = preview.poolDigest?.trim() ?? '';
  return digest || null;
}
