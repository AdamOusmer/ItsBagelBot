// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MAX_ACTIONS = 20;
export const MAX_CONDITIONS = 10;
export const MAX_DELAY_MS = 60000;
export const MAX_CHAT_MESSAGES_PER_RUN = 3;
export const MAX_RECURSION_DEPTH = 2;
export const MAX_NAME_LENGTH = 64;
export const MAX_AUTOMATIONS_PER_CHANNEL = 100;
export const MAX_DEFINITION_BYTES = 32768;
export const MAX_PREVIEW_BYTES = 4096;
export const MAX_FLOWS_PER_FILE = MAX_AUTOMATIONS_PER_CHANNEL;
export const MAX_FLOW_FILE_BYTES = 4 * 1024 * 1024;
export const RUNS_KEPT = 50;
export const MAX_RUNS_PER_MINUTE = 60;

export const MAX_TEMPLATE_LENGTH = 500;
export const MAX_LOGIN_TEMPLATE = 100;
export const MAX_EVENT_FILTERS = 10;
export const MAX_FILTER_VALUE = 100;
export const MAX_COMMAND_ALIASES = 10;
export const MAX_LIST_ENTRIES = 100;
export const MAX_PATTERN_LENGTH = 200;
export const MAX_FETCH_URL_LENGTH = 512;

export const VERSION = 1;

export const CODE = {
  required: 'required',
  tooLong: 'too_long',
  tooShort: 'too_short',
  outOfRange: 'out_of_range',
  invalidEnum: 'invalid_enum',
  invalidFormat: 'invalid_format',
  invalidRegex: 'invalid_regex',
  unknownType: 'unknown_type',
  unknownField: 'unknown_field',
  unknownEventKind: 'unknown_event_kind',
  unknownFilter: 'unknown_filter',
  tooMany: 'too_many',
  nesting: 'nesting',
  delayBudget: 'delay_budget',
  triggerMismatch: 'trigger_mismatch',
  permRequired: 'perm_required',
  invalidJson: 'invalid_json',
  unsupportedVersion: 'unsupported_version',
} as const;

export type IssueCode = (typeof CODE)[keyof typeof CODE];
