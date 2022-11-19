package fox

import (
	"sync"
	"sync/atomic"
)

var (
	paramsPool = sync.Pool{
		New: func() interface{} {
			params := make(Params, 0, atomic.LoadUint32(&maxParams))
			return &params
		},
	}
	maxParams uint32
)

type Param struct {
	Key   string
	Value string
}

type Params []Param

// Get the matching wildcard segment by name.
func (p *Params) Get(name string) string {
	for i := range *p {
		if (*p)[i].Key == name {
			return (*p)[i].Value
		}
	}
	return ""
}

func newParams() *Params {
	return paramsPool.Get().(*Params)
}

func (p *Params) free() {
	if cap(*p) < int(atomic.LoadUint32(&maxParams)) {
		return
	}

	*p = (*p)[:0]
	paramsPool.Put(p)
}

// updateMaxParams perform an update only if max is greater than the current
// max params. This function should be guarded by mutex.
func updateMaxParams(max uint32) {
	if max > atomic.LoadUint32(&maxParams) {
		atomic.StoreUint32(&maxParams, max)
	}
}
