// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics

type SnapshotCell struct {
	Window string   `json:"window"`
	Start  string   `json:"start"`
	Metric Metric   `json:"metric"`
	Dims   []string `json:"dims"`
	Value  int64    `json:"value"`
}

type Snapshot struct {
	Version int            `json:"version"`
	Rows    []SnapshotCell `json:"rows"`
}
