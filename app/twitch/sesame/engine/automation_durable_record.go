// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
)

type durableBudget struct {
	DelayMS int `json:"delay_ms"`
	Segment int `json:"segment"`
	Handled int `json:"handled"`
}

func (b durableBudget) budget() runBudget {
	return runBudget{delayMS: b.DelayMS, segment: b.Segment, handled: b.Handled}
}

type durableTree struct {
	Executed int32 `json:"executed"`
	Visits   int32 `json:"visits"`
}

func snapshotTree(tree *runTree) durableTree {
	if tree == nil {
		return durableTree{}
	}
	return durableTree{Executed: tree.executed.Load(), Visits: tree.visits.Load()}
}

func (t durableTree) tree() *runTree {
	tree := new(runTree)
	tree.executed.Store(t.Executed)
	tree.visits.Store(t.Visits)
	return tree
}

// Path alternates step and branch indexes from the plan root and ends on the first remaining step, or on the loop step.
type durableFrame struct {
	Path []int        `json:"path"`
	Iter string       `json:"iter,omitempty"`
	Loop *durableLoop `json:"loop,omitempty"`
}

type durableLoop struct {
	Next     int    `json:"next"`
	Total    int    `json:"total"`
	Prior    string `json:"prior,omitempty"`
	Shadowed bool   `json:"shadowed,omitempty"`
}

type durableRecord struct {
	AutomationID string                  `json:"automation_id"`
	Revision     int64                   `json:"revision"`
	Frames       []durableFrame          `json:"frames,omitempty"`
	Waiting      bool                    `json:"waiting,omitempty"`
	WaitUntil    int64                   `json:"wait_until,omitempty"`
	ResumeAt     int64                   `json:"resume_at,omitempty"`
	Seat         *RunQueueSeat           `json:"seat,omitempty"`
	RunID        string                  `json:"run_id"`
	Identity     string                  `json:"identity,omitempty"`
	Replay       string                  `json:"replay"`
	Source       string                  `json:"source,omitempty"`
	Channel      string                  `json:"channel,omitempty"`
	Locale       string                  `json:"locale,omitempty"`
	Regress      module.Regress          `json:"regress"`
	Role         module.Role             `json:"role"`
	Depth        int                     `json:"depth,omitempty"`
	Viewer       Viewer                  `json:"viewer"`
	Args         string                  `json:"args,omitempty"`
	ArgMap       map[string]string       `json:"arg_map,omitempty"`
	Event        map[string]string       `json:"event,omitempty"`
	Locals       map[string]string       `json:"locals,omitempty"`
	Iter         string                  `json:"iter,omitempty"`
	Started      time.Time               `json:"started"`
	Env          *lane.Envelope          `json:"env,omitempty"`
	Record       automationrpc.RunRecord `json:"record"`
	Budget       durableBudget           `json:"budget"`
	Tree         durableTree             `json:"tree"`
}

func baseDurableRecord(run *AutomationRun) durableRecord {
	return durableRecord{
		AutomationID: run.Plan.Automation.ID,
		Revision:     run.Plan.Automation.Revision,
		RunID:        run.ID,
		Identity:     run.Identity,
		Replay:       run.replayKey(),
		Source:       run.Source,
		Channel:      run.Channel,
		Locale:       run.Locale,
		Regress:      run.Regress,
		Role:         run.DetachedRole,
		Depth:        run.Depth,
		Viewer:       run.Viewer,
		Args:         run.Args,
		ArgMap:       run.ArgMap,
		Event:        run.Event,
		Locals:       run.Locals,
		Iter:         run.Iter,
		Started:      run.Started,
		Env:          run.Env,
		Record:       run.Record,
		Tree:         snapshotTree(run.Tree),
	}
}

func newDurableRecord(c continuation) (durableRecord, bool) {
	rec := baseDurableRecord(c.run)
	rec.ResumeAt = time.Now().Add(c.wait).UnixMilli()
	rec.Budget = durableBudget{DelayMS: c.budget.delayMS, Segment: c.budget.segment, Handled: c.budget.handled}
	if c.run.Slot != nil {
		seat := c.run.Slot.seat
		rec.Seat = &seat
	}
	rec.Frames = make([]durableFrame, 0, len(c.rest))
	for _, r := range c.rest {
		frame, ok := newDurableFrame(c.run.Plan.Steps, r)
		if !ok {
			return rec, false
		}
		rec.Frames = append(rec.Frames, frame)
	}
	return rec, true
}

func newDurableWaiter(run *AutomationRun, turn queueTurn) durableRecord {
	rec := baseDurableRecord(run)
	rec.Waiting = true
	rec.WaitUntil = turn.until.UnixMilli()
	seat := turn.seat
	rec.Seat = &seat
	return rec
}

func newDurableFrame(root []automation.Step, r resume) (durableFrame, bool) {
	if r.loop != nil {
		path, ok := planPath(root, r.loop.step, nil)
		loop := &durableLoop{Next: r.loop.next, Total: r.loop.total, Prior: r.loop.prior, Shadowed: r.loop.shadowed}
		return durableFrame{Path: path, Iter: r.iter, Loop: loop}, ok
	}
	if len(r.steps) == 0 {
		return durableFrame{}, false
	}
	path, ok := planPath(root, &r.steps[0], nil)
	return durableFrame{Path: path, Iter: r.iter}, ok
}

func planPath(list []automation.Step, target step, prefix []int) ([]int, bool) {
	for i := range list {
		if &list[i] == target {
			return append(prefix, i), true
		}
		if path, ok := branchPath(&list[i], i, target, prefix); ok {
			return path, true
		}
	}
	return nil, false
}

func branchPath(s step, index int, target step, prefix []int) ([]int, bool) {
	for b := range s.Branches {
		nested := append(append(make([]int, 0, len(prefix)+2), prefix...), index, b)
		if path, ok := planPath(s.Branches[b], target, nested); ok {
			return path, true
		}
	}
	return nil, false
}

func (r *durableRecord) run(ref DurableRef) *AutomationRun {
	return &AutomationRun{
		BroadcasterID: ref.BroadcasterID,
		Channel:       r.channel(ref),
		Regress:       r.Regress,
		Locale:        r.Locale,
		Env:           r.Env,
		Viewer:        r.Viewer,
		Args:          r.Args,
		ArgMap:        r.ArgMap,
		Event:         r.Event,
		Source:        r.Source,
		ID:            r.RunID,
		Identity:      r.Identity,
		Depth:         r.Depth,
		Started:       r.Started,
		Record:        r.Record,
		Locals:        r.Locals,
		Iter:          r.Iter,
		Replay:        r.Replay,
		Tree:          r.Tree.tree(),
		DetachedRole:  r.Role,
		Durable:       &durableTicket{ref: ref, ticket: r.ticket()},
	}
}

func (r *durableRecord) channel(ref DurableRef) string {
	if r.Channel != "" {
		return r.Channel
	}
	return strconv.FormatUint(ref.BroadcasterID, 10)
}

func (r *durableRecord) ticket() string {
	if r.Seat == nil {
		return ""
	}
	return r.Seat.Ticket
}

// A nil plan or another revision means the recorded positions may point at different steps.
func (r *durableRecord) frames(plan *automation.Plan) ([]resume, bool) {
	if plan == nil || plan.Automation.Revision != r.Revision {
		return nil, false
	}
	frames := make([]resume, 0, len(r.Frames))
	for _, f := range r.Frames {
		frame, ok := f.resume(plan.Steps)
		if !ok {
			return nil, false
		}
		frames = append(frames, frame)
	}
	return frames, true
}

func (f durableFrame) resume(root []automation.Step) (resume, bool) {
	tail, ok := stepsAt(root, f.Path)
	if !ok {
		return resume{}, false
	}
	if f.Loop == nil {
		return resume{steps: tail, iter: f.Iter}, true
	}
	if !isLoopStep(&tail[0]) {
		return resume{}, false
	}
	cursor := &loopCursor{step: &tail[0], next: f.Loop.Next, total: f.Loop.Total, prior: f.Loop.Prior, shadowed: f.Loop.Shadowed}
	return resume{loop: cursor, iter: f.Iter}, true
}

func isLoopStep(s step) bool {
	return s.Type == "loop" && len(s.Branches) > 0
}

func stepsAt(root []automation.Step, path []int) ([]automation.Step, bool) {
	if len(path)%2 == 0 {
		return nil, false
	}
	list := root
	for i := 0; i < len(path)-1; i += 2 {
		branches, ok := branchesAt(list, path[i])
		if !ok || path[i+1] < 0 || path[i+1] >= len(branches) {
			return nil, false
		}
		list = branches[path[i+1]]
	}
	last := path[len(path)-1]
	if last < 0 || last >= len(list) {
		return nil, false
	}
	return list[last:], true
}

func branchesAt(list []automation.Step, index int) ([][]automation.Step, bool) {
	if index < 0 || index >= len(list) {
		return nil, false
	}
	return list[index].Branches, true
}
