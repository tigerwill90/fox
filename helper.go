package fox

import (
	"context"
	"net/http"
)

type key struct{}

var ParamsKey = key{}

func ParamsFromContext(ctx context.Context) Params {
	p, _ := ctx.Value(ParamsKey).(Params)
	return p
}

func WrapF(f http.HandlerFunc) Handler {
	return HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
		if len(params) > 0 {
			f.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ParamsKey, params)))
			return
		}
		f.ServeHTTP(w, r)
	})
}

func WrapH(h http.Handler) Handler {
	return HandlerFunc(func(w http.ResponseWriter, r *http.Request, params Params) {
		if len(params) > 0 {
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ParamsKey, params)))
			return
		}
		h.ServeHTTP(w, r)
	})
}
