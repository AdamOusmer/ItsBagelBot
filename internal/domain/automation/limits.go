// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

const (
	MaxActions               = 1000
	MaxDelayMS               = 60000
	MaxRecursionDepth        = 2
	MaxNameLength            = 64
	MaxAutomationsPerChannel = 100
	MaxDefinitionBytes       = 32768
	MaxPreviewBytes          = 4096
	RunsKept                 = 50
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
	MaxVariableValueRunes  = 500
	MaxChannelVariables    = 200
	MaxViewerVariableNames = 200
	MaxVariableAmount      = 1_000_000_000_000_000
	MaxSavedValueRunes     = 500
	MaxTextOperandRunes    = 1000
	MaxMathExpressionRunes = 200
	MaxLoopTimes           = 100
	MaxStepVisitsPerRun    = 4 * MaxActions
	MaxRecordedActions     = 1200
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
	CodeDelayBudget      = "delay_budget"
	CodeTriggerMismatch  = "trigger_mismatch"
	CodePermRequired     = "perm_required"
	CodeInvalidJSON      = "invalid_json"
	CodeUnsupportedVer   = "unsupported_version"
)
