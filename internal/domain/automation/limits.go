// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

const (
	MaxActions               = 20
	MaxConditions            = 10
	MaxDelayMS               = 10000
	MaxChatMessagesPerRun    = 3
	MaxRecursionDepth        = 2
	MaxNameLength            = 64
	MaxAutomationsPerChannel = 100
	MaxDefinitionBytes       = 32768
	MaxPreviewBytes          = 4096
	RunsKept                 = 50
	MaxRunsPerMinute         = 60
)

const (
	MaxTemplateLength = 500
	MaxLoginTemplate  = 100
	MaxEventFilters   = 10
	MaxFilterValue    = 100
	MaxCommandAliases = 10
	MaxListEntries    = 100
	MaxPatternLength  = 200
)

const (
	CodeRequired         = "required"
	CodeTooLong          = "too_long"
	CodeTooShort         = "too_short"
	CodeOutOfRange       = "out_of_range"
	CodeInvalidEnum      = "invalid_enum"
	CodeInvalidFormat    = "invalid_format"
	CodeInvalidRegex     = "invalid_regex"
	CodeUnknownType      = "unknown_type"
	CodeUnknownField     = "unknown_field"
	CodeUnknownEventKind = "unknown_event_kind"
	CodeUnknownFilter    = "unknown_filter"
	CodeTooMany          = "too_many"
	CodeNesting          = "nesting"
	CodeDelayBudget      = "delay_budget"
	CodeTriggerMismatch  = "trigger_mismatch"
	CodePermRequired     = "perm_required"
	CodeInvalidJSON      = "invalid_json"
	CodeUnsupportedVer   = "unsupported_version"
)
