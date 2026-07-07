package automod

import (
	"math/bits"
	"testing"
)

func TestSimHashBagOfWords(t *testing.T) {
	a := simHash([]byte("free nitro at example com click now"))
	b := simHash([]byte("click now free nitro at example com")) // reordered
	if a == 0 || a != b {
		t.Fatalf("token order must not change the hash: %x vs %x", a, b)
	}
}

func TestSimHashNearDuplicate(t *testing.T) {
	a := simHash([]byte("free nitro giveaway at example com click here fast friends"))
	b := simHash([]byte("free nitro giveaway at other com click here fast friends")) // one token swapped
	c := simHash([]byte("what a great play by the jungler that was insane"))

	if d := bits.OnesCount64(a ^ b); d > 24 {
		t.Fatalf("near-duplicates too far apart: hamming=%d", d)
	}
	if a == c {
		t.Fatal("unrelated messages must not collide")
	}
}

func TestSimHashEmpty(t *testing.T) {
	if simHash(nil) != 0 || simHash([]byte("   ")) != 0 {
		t.Fatal("no tokens must hash to 0 (reserved)")
	}
	if simHash([]byte("x")) == 0 {
		t.Fatal("a real token must never hash to the reserved 0")
	}
}

func TestSimHashZeroAlloc(t *testing.T) {
	skel := []byte("free nitro giveaway at example com click here fast friends")
	allocs := testing.AllocsPerRun(200, func() { _ = simHash(skel) })
	if allocs != 0 {
		t.Fatalf("simHash allocated %.1f/op, want 0", allocs)
	}
}

func TestSimBands(t *testing.T) {
	h := uint64(0xaabbccdd11223344)
	b1, b2 := simBands(h)
	if b1 != 0xaabbccdd || b2 != 0x11223344 {
		t.Fatalf("bands = %x, %x", b1, b2)
	}
}
