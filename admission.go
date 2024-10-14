package fox

import (
	"context"
	"iter"
)

type ValidationTree interface {
	Has(method, path string) bool
	Route(method, path string) *Route
}

type MutationTree interface {
	ValidationTree
	Handle(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Update(method, path string, handler HandlerFunc, opts ...PathOption) (*Route, error)
	Delete(method, path string) error
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

type AdmissionController interface {
	Admit(ctx context.Context, tree MutationTree, route MutationRoute) (err error)
	Validate(ctx context.Context, tree ValidationTree, route ValidationRoute) (err error)
}
