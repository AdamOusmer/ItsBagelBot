// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package objectstoretest_test

import (
	"testing"

	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/objectstore/objectstoretest"
)

func TestMemorySatisfiesStoreContract(t *testing.T) {
	objectstoretest.Contract(t, func(*testing.T) objectstore.Store { return objectstoretest.NewMemory() })
}
