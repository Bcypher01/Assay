package primitive

import (
	"sync"
)

// zoneChunks recycles the backing arrays of evicted KindZone Values. A cached
// Value is immutable for as long as it is reachable; once MemoCache evicts an
// entry no reader can reach it, so its array is dead and safe to hand to the
// next cold detection pass.
//
// Recycled arrays are NOT re-zeroed. Detectors write every slot they mark valid,
// and every consumer of a KindZone Value reads a slot only when Valid is set for
// that bar. Keep it that way: a detector that emits an uninitialised position and
// a reader that does not gate on Valid will corrupt a run.
var zoneChunks sync.Pool

// AllocZones returns a []Zone of length n, reusing an evicted array when one of
// sufficient capacity is available. The caller must treat the result exactly
// like the make it replaces, with one extra constraint: once the slice is part
// of a Value that reaches MemoCache, it must not be written to again.
func AllocZones(n int) []Zone {
	if n == 0 {
		return nil
	}
	if p, ok := zoneChunks.Get().(*[]Zone); ok && cap(*p) >= n {
		return (*p)[:n]
	}
	return make([]Zone, n)
}

// releaseZones returns an evicted Value's backing array to the pool. The caller
// (MemoCache) must already have made the array unreachable.
func releaseZones(buf []Zone) {
	if len(buf) > 0 {
		zoneChunks.Put(&buf)
	}
}
