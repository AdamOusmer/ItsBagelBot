// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

type creditCosts [][]int

type creditAssignment struct {
	cost                 creditCosts
	rows, columns        []int
	owners, predecessors []int
}

func (cost creditCosts) minimum(groups, credits []matchPhrase) artistMatch {
	width := len(cost[0]) + 1
	a := creditAssignment{
		cost: cost, rows: make([]int, len(cost)+1), columns: make([]int, width),
		owners: make([]int, width), predecessors: make([]int, width),
	}
	for row := 1; row <= len(cost); row++ {
		a.augment(row)
	}
	return a.evidence(groups, credits)
}

func (a creditAssignment) evidence(groups, credits []matchPhrase) artistMatch {
	match := artistMatch{level: artistFull, index: -1}
	for col, row := range a.owners[1:] {
		if row == 0 {
			continue
		}
		value := a.cost[row-1][col]
		if value == noCredit {
			return artistMatch{}
		}
		match.addCredit(artistEdit{groups[row-1], credits[col]}, col, value)
	}
	return match
}

func (m *artistMatch) addCredit(edit artistEdit, index, cost int) {
	if cost == partialCreditCost {
		m.addPartial(edit.query, index)
		return
	}
	m.addEdit(edit, cost)
}

func (m *artistMatch) addPartial(group matchPhrase, index int) {
	m.index, m.partial = index, group
	if m.level == artistPartial {
		m.index, m.partial = -1, nil
	}
	m.level = artistPartial
	m.combination = true
}

func (a creditAssignment) augment(row int) {
	a.owners[0] = row
	minimum := make([]int, len(a.columns))
	visited := make([]bool, len(a.columns))
	for col := 1; col < len(minimum); col++ {
		minimum[col] = noCredit
	}
	col := 0
	for a.owners[col] != 0 {
		visited[col] = true
		next, change := a.relax(creditPath{col, minimum, visited})
		a.adjust(creditPath{col, minimum, visited}, change)
		col = next
	}
	for col != 0 {
		previous := a.predecessors[col]
		a.owners[col] = a.owners[previous]
		col = previous
	}
}

type creditPath struct {
	col     int
	minimum []int
	visited []bool
}

func (a creditAssignment) relax(path creditPath) (int, int) {
	owner := a.owners[path.col]
	next, change := 0, noCredit+1
	for col := 1; col < len(a.columns); col++ {
		if path.visited[col] {
			continue
		}
		value := a.cost[owner-1][col-1] - a.rows[owner] - a.columns[col]
		if value < path.minimum[col] {
			path.minimum[col], a.predecessors[col] = value, path.col
		}
		if path.minimum[col] < change {
			next, change = col, path.minimum[col]
		}
	}
	return next, change
}

func (a creditAssignment) adjust(path creditPath, change int) {
	for col, visited := range path.visited {
		if visited {
			a.rows[a.owners[col]] += change
			a.columns[col] -= change
		} else {
			path.minimum[col] -= change
		}
	}
}
