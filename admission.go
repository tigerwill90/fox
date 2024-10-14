package fox

import (
	"cmp"
	"context"
	"iter"
	"slices"
)

type hook struct {
	ctx context.Context
}

type ValidationTree interface {
	Has(method, path string) bool
	Route(method, path string) *Route
}

type MutationTree interface {
	ValidationTree
	Handle(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Update(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Delete(method, path string, opts ...AdmissionOption) error
}

type ValidationRoute interface {
	Path() string
	Annotations() iter.Seq[Annotation]
	RedirectTrailingSlashEnabled() bool
	IgnoreTrailingSlashEnabled() bool
	ClientIPStrategyEnabled() bool
}

type MutationRoute interface {
	ValidationRoute
	SetPath(path string)
	PatchAnnotation(fn func(annotations iter.Seq[Annotation]) iter.Seq[Annotation])
	// TODO Patch middleware
	SetRedirectTrailingSlash(enable bool)
	SetIgnoreTrailingSlash(enable bool)
	SetClientIPStrategy(strategy ClientIPStrategy)
}

type mutationRoute struct {
	*Route
}

func (r mutationRoute) SetPath(path string) {
	r.path = path
}

func (r mutationRoute) PatchAnnotation(fn func(annotations iter.Seq[Annotation]) iter.Seq[Annotation]) {
	r.annots = slices.Collect(fn(slices.Values(r.annots)))
}

func (r mutationRoute) SetRedirectTrailingSlash(enable bool) {
	r.redirectTrailingSlash = enable
}

func (r mutationRoute) SetIgnoreTrailingSlash(enable bool) {
	r.ignoreTrailingSlash = enable
}

func (r mutationRoute) SetClientIPStrategy(strategy ClientIPStrategy) {
	r.ipStrategy = cmp.Or(strategy, ClientIPStrategy(noClientIPStrategy{}))
}

type AdmissionController interface {
	Handles(operation Operation) bool
	Admit(ctx context.Context, tree MutationTree, route MutationRoute) (err error)
	Validate(ctx context.Context, tree ValidationTree, route ValidationRoute) (err error)
}

type Operation uint8

const (
	Insert Operation = iota
	Update
	Delete
)
