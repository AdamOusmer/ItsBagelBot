package automod

// A compact Aho-Corasick matcher: one pass over the text finds whether any of a
// set of byte patterns occurs, replacing the per-term bytes.Contains loop once a
// category holds more than a handful of terms. Built once per lexicon load
// (read-only afterwards, safe for concurrent matching); matching allocates
// nothing. ~100 lines beats importing a dependency for exactly this.
type acNode struct {
	next map[byte]int32
	fail int32
	out  int32 // index of a pattern ending here (or inherited via fail), -1 none
}

type matcher struct {
	nodes []acNode
}

// newMatcher builds the trie plus BFS failure links. Patterns must be non-empty;
// on overlapping matches the earliest-added pattern wins, which is fine because
// callers only need "which term hit", not every occurrence.
func newMatcher(patterns [][]byte) *matcher {
	m := &matcher{nodes: []acNode{{next: map[byte]int32{}, out: -1}}}
	for pi, p := range patterns {
		cur := int32(0)
		for _, b := range p {
			nxt, ok := m.nodes[cur].next[b]
			if !ok {
				m.nodes = append(m.nodes, acNode{next: map[byte]int32{}, out: -1})
				nxt = int32(len(m.nodes) - 1)
				m.nodes[cur].next[b] = nxt
			}
			cur = nxt
		}
		if m.nodes[cur].out < 0 {
			m.nodes[cur].out = int32(pi)
		}
	}

	// BFS: children of the root fail to the root; deeper nodes fail to the
	// longest proper suffix present in the trie, inheriting its output so a
	// pattern that is a suffix of another is still found.
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
			if m.nodes[v].out < 0 {
				m.nodes[v].out = m.nodes[m.nodes[v].fail].out
			}
			queue = append(queue, v)
		}
	}
	return m
}

// find returns the index of the first pattern found in text, or -1. Single pass,
// zero allocations.
func (m *matcher) find(text []byte) int {
	cur := int32(0)
	for _, b := range text {
		cur = m.step(cur, b)
		if out := m.nodes[cur].out; out >= 0 {
			return int(out)
		}
	}
	return -1
}

// step advances the automaton by one byte, following failure links.
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

// foldTable maps raw ascii bytes onto the skeleton alphabet the lexicon
// patterns are written in: letters lowercase, common leet digits/symbols fold
// to their letters, every other ascii byte becomes a space (so punctuation is
// a word boundary, matching the skeleton's collapse), and non-ascii bytes map
// to an unmatchable sentinel. This is skeleton-lite for the clean-path
// pre-scan: cheap enough to run per byte with no allocation.
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
			t[i] = 0xff // non-ascii: never matches an ascii pattern
		}
	}
	t['0'], t['1'], t['3'], t['4'], t['5'], t['7'], t['8'] = 'o', 'i', 'e', 'a', 's', 't', 'b'
	t['@'], t['$'] = 'a', 's'
	return t
}()

// findFolded runs the automaton over text with foldTable applied per byte and
// virtual space padding on both ends, so the space-padded word-bounded
// patterns match raw chat text directly. Zero allocations: this is the
// clean-path pre-scan.
func (m *matcher) findFolded(text string) bool {
	cur := m.step(0, ' ')
	for i := 0; i < len(text); i++ {
		cur = m.step(cur, foldTable[text[i]])
		if m.nodes[cur].out >= 0 {
			return true
		}
	}
	cur = m.step(cur, ' ')
	return m.nodes[cur].out >= 0
}
