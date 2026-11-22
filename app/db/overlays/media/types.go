// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

type blob []byte

type offset int

func (b blob) end() offset {
	return offset(len(b))
}
