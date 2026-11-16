// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

type LevelService string

const (
	UsersLevels    LevelService = "users"
	ModulesLevels  LevelService = "modules"
	CommandsLevels LevelService = "commands"
)

func (s LevelService) LevelsSubject() string {
	return "rpc.internal." + string(s) + ".analytics.levels"
}

type LevelsRequest struct{}

type LevelsReply struct {
	Refusal
	Levels map[string]int64 `json:"levels"`
}
