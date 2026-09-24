// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

const noPattern = -1

type acNode struct {
	next           map[byte]int32
	fail           int32
	matchedPattern int32
}

type matcher struct {
	nodes []acNode
}

func newMatcher(patterns [][]byte) *matcher {
	m := &matcher{nodes: []acNode{{next: map[byte]int32{}, matchedPattern: noPattern}}}
	for pi, p := range patterns {
		cur := int32(0)
		for _, b := range p {
			nxt, ok := m.nodes[cur].next[b]
			if !ok {
				m.nodes = append(m.nodes, acNode{next: map[byte]int32{}, matchedPattern: noPattern})
				nxt = int32(len(m.nodes) - 1)
				m.nodes[cur].next[b] = nxt
			}
			cur = nxt
		}
		if m.nodes[cur].matchedPattern < 0 {
			m.nodes[cur].matchedPattern = int32(pi)
		}
	}

	queue := make([]int32, 0, len(m.nodes))
	for _, v := range m.nodes[0].next {
		queue = append(queue, v)
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for b, v := range m.nodes[u].next {
			f := m.nodes[u].fail
			for f != 0 {
				if _, ok := m.nodes[f].next[b]; ok {
					break
				}
				f = m.nodes[f].fail
			}
			if w, ok := m.nodes[f].next[b]; ok && w != v {
				m.nodes[v].fail = w
			} else {
				m.nodes[v].fail = 0
			}
			if m.nodes[v].matchedPattern < 0 {
				m.nodes[v].matchedPattern = m.nodes[m.nodes[v].fail].matchedPattern
			}
			queue = append(queue, v)
		}
	}
	return m
}

func (m *matcher) find(text []byte) int {
	cur := int32(0)
	for _, b := range text {
		cur = m.step(cur, b)
		if matched := m.nodes[cur].matchedPattern; matched >= 0 {
			return int(matched)
		}
	}
	return noPattern
}

func (m *matcher) step(cur int32, b byte) int32 {
	for cur != 0 {
		if _, ok := m.nodes[cur].next[b]; ok {
			break
		}
		cur = m.nodes[cur].fail
	}
	if v, ok := m.nodes[cur].next[b]; ok {
		return v
	}
	return cur
}

var foldTable = func() [256]byte {
	var t [256]byte
	for i := 0; i < 256; i++ {
		switch b := byte(i); {
		case b >= 'a' && b <= 'z':
			t[i] = b
		case b >= 'A' && b <= 'Z':
			t[i] = b + ('a' - 'A')
		case b < 0x80:
			t[i] = ' '
		default:
			t[i] = 0xff
		}
	}
	t['0'], t['1'], t['3'], t['4'], t['5'], t['7'], t['8'] = 'o', 'i', 'e', 'a', 's', 't', 'b'
	t['@'], t['$'] = 'a', 's'
	return t
}()

func (m *matcher) findFolded(text string) bool {
	cur := m.step(0, ' ')
	for i := 0; i < len(text); i++ {
		cur = m.step(cur, foldTable[text[i]])
		if m.nodes[cur].matchedPattern >= 0 {
			return true
		}
	}
	cur = m.step(cur, ' ')
	return m.nodes[cur].matchedPattern >= 0
}
