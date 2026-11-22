// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package overlaysrpc

import (
	"encoding/json"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	AssetBeginSubject  = "rpc.overlays.assets.begin"
	AssetListSubject   = "rpc.overlays.assets.list"
	AssetDeleteSubject = "rpc.overlays.assets.delete"
	ImportBeginSubject = "rpc.overlays.imports.begin"

	OverlayCreateCustomSubject = "rpc.overlays.overlay.create_custom"

	UploadPartSize = 32 << 20

	AssetUploadPath  = "/assets/"
	ImportUploadPath = "/imports/"
	DryRunQuery      = "dry_run"
	PartQuery        = "part"
	DoneQuery        = "done"
)

type AssetBeginRequest struct {
	OverlayUserRequest
	DisplayName  string `json:"display_name"`
	DeclaredSize int64  `json:"declared_size"`
}

type ImportBeginRequest struct {
	OverlayUserRequest
	DeclaredSize int64 `json:"declared_size"`
	DryRun       bool  `json:"dry_run,omitempty"`
}

type UploadTicket struct {
	Ticket   string `json:"ticket"`
	MaxSize  int64  `json:"max_size"`
	Expires  int64  `json:"expires"`
	PartSize int64  `json:"part_size"`
	Parts    int    `json:"parts"`
	rpc.Refusal
}

type AssetView struct {
	ID          string `json:"asset_id"`
	URL         string `json:"url"`
	Type        string `json:"type"`
	Bytes       int64  `json:"bytes"`
	W           int    `json:"w"`
	H           int    `json:"h"`
	DurationMS  int    `json:"duration_ms"`
	DisplayName string `json:"display_name,omitempty"`
}

type AssetListReply struct {
	Assets []AssetView `json:"assets"`
	rpc.Refusal
}

type AssetRefRequest struct {
	OverlayUserRequest
	ID string `json:"asset_id"`
}

type AssetDeleteReply struct {
	rpc.Refusal
}

type ImportItem struct {
	Subject string `json:"subject"`
	Reason  string `json:"reason,omitempty"`
}

type ImportReport struct {
	Mapped       []ImportItem `json:"mapped"`
	Approximated []ImportItem `json:"approximated"`
	Unsupported  []ImportItem `json:"unsupported"`
}

type ImportResult struct {
	OverlayID  uint64       `json:"overlay_id,omitempty"`
	OverlayIDs []uint64     `json:"overlay_ids,omitempty"`
	URL        string       `json:"url,omitempty"`
	Report     ImportReport `json:"report"`
}

type OverlayCreateCustomRequest struct {
	OverlayUserRequest
	Document json.RawMessage `json:"document"`
}
