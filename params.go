package fox

import (
	"context"
	"net/http"
	"sync/atomic"
)

const RouteKey = "$k/fox"

var ParamsKey = key{}

type key struct{}

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

// Clone make a copy of Params.
func (p *Params) Clone() Params {
	cloned := make(Params, len(*p))
	copy(cloned, *p)
	return cloned
}

func (fox *Router) newParams() *Params {
	return fox.p.Get().(*Params)
}

func (p *Params) free(fox *Router) {
	if cap(*p) < int(atomic.LoadUint32(&fox.maxParams)) {
		return
	}

	*p = (*p)[:0]
	fox.p.Put(p)
}

// updateMaxParams perform an update only if max is greater than the current
// max params. This function should be guarded by mutex.
func (fox *Router) updateMaxParams(max uint32) {
	if max > atomic.LoadUint32(&fox.maxParams) {
		atomic.StoreUint32(&fox.maxParams, max)
	}
}

// ParamsFromContext is a helper function to retrieve parameters from the request context.
func ParamsFromContext(ctx context.Context) Params {
	p, _ := ctx.Value(ParamsKey).(Params)
	return p
}

// WrapF is a helper function for wrapping http.HandlerFunc and returns a Fox Handler.
// Params are forwarded via the request context. See ParamsFromContext to retrieve parameters.
func WrapF(f http.HandlerFunc) Handler {
	return HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
		if len(params) > 0 {
			f.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ParamsKey, params)))
			return
		}
		f.ServeHTTP(w, r)
	})
}

// WrapH is a helper function for wrapping http.Handler and returns a Fox Handler.
// Params are forwarded via the request context. See ParamsFromContext to retrieve parameters.
func WrapH(h http.Handler) Handler {
	return HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
		if len(params) > 0 {
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ParamsKey, params)))
			return
		}
		h.ServeHTTP(w, r)
	})
}
