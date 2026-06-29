package ratelimit

import "github.com/puzpuzpuz/xsync/v4"

// BucketStore is the process-local table of active lease buckets. Lease
// lifetimes are controlled by plans, so a general-purpose TTL cache adds work
// without adding correctness. xsync.Map provides typed, allocation-free reads
// and scales better than a single locked map under outgress concurrency.
type BucketStore struct {
	buckets *xsync.Map[BucketID, *LocalBucket]
}

func NewBucketStore(sizeHint int) *BucketStore {
	return &BucketStore{buckets: xsync.NewMap[BucketID, *LocalBucket](xsync.WithPresize(sizeHint))}
}

func (s *BucketStore) Load(key BucketID) (*LocalBucket, bool) {
	return s.buckets.Load(key)
}

func (s *BucketStore) LoadOrStore(key BucketID, value *LocalBucket) (*LocalBucket, bool) {
	return s.buckets.LoadOrStore(key, value)
}

func (s *BucketStore) Store(key BucketID, value *LocalBucket) {
	s.buckets.Store(key, value)
}

func (s *BucketStore) Delete(key BucketID) {
	s.buckets.Delete(key)
}

func (s *BucketStore) DeleteExpired(nowUnixNano int64) {
	s.buckets.DeleteMatching(func(_ BucketID, bucket *LocalBucket) (bool, bool) {
		return bucket.ExpiredUnixNano(nowUnixNano), false
	})
}
