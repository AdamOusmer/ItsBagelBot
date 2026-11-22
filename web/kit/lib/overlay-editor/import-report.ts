// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const IMPORT_REASONS = [
  'license_file', 'convert_to_webm', 'convert_to_woff2', 'rejected_type', 'unsupported_type', 'code_outside_bundle',
  'empty_file', 'file_too_large', 'too_many_assets', 'too_many_of_type', 'too_many_widgets', 'not_in_manifest',
  'unknown_widget', 'missing_widget_code', 'widget_not_text', 'tips_merch', 'points_store', 'contests_bets',
  'media_share', 'key_value_store', 'remote_font', 'network_call', 'approximated_font', 'unknown_field_type'
] as const;
export type ImportReason = (typeof IMPORT_REASONS)[number];

export const IMPORT_ERRORS = [
  'invalid_archive', 'unsafe_path', 'symlink', 'nested_archive', 'too_many_entries', 'too_large', 'bomb_ratio',
  'invalid_manifest', 'invalid_document', 'invalid_bundle', 'bad_hash', 'missing_asset', 'premium_required', 'failed'
] as const;
export type ImportError = (typeof IMPORT_ERRORS)[number];

export class ImportFailure extends Error {
  constructor(readonly code: ImportError) {
    super(code);
  }
}

export type ImportItem = { subject: string; reason?: string };
export type ImportReport = { mapped: ImportItem[]; approximated: ImportItem[]; unsupported: ImportItem[] };
export type ImportResult = { report: ImportReport; overlay_id?: number; overlay_ids?: number[]; url?: string };

export const REPORT_GROUPS = ['mapped', 'approximated', 'unsupported'] as const;
export type ReportGroup = (typeof REPORT_GROUPS)[number];

export function importReason(code: string | undefined): ImportReason | undefined {
  return (IMPORT_REASONS as readonly string[]).includes(code ?? '') ? (code as ImportReason) : undefined;
}

export function importError(code: string): ImportError {
  return (IMPORT_ERRORS as readonly string[]).includes(code) ? (code as ImportError) : 'failed';
}

export function reportCounts(report: ImportReport): Record<ReportGroup, number> {
  return { mapped: report.mapped.length, approximated: report.approximated.length, unsupported: report.unsupported.length };
}
