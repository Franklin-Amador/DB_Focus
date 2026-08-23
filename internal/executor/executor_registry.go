package executor

import (
	"context"
	"reflect"

	"dbf/internal/ast"
)

// execHandler is the type-erased form of a statement handler stored in the
// dispatch registry. The concrete AST type is recovered by the wrapper that
// registerExec builds.
type execHandler func(*Executor, context.Context, ast.Statement) (*Result, error)

// execHandlers maps a concrete *ast.X statement type to its handler.
// It is populated by registerExec calls from init() blocks in the various
// executor_*.go files, so a new statement type registers itself next to its
// implementation instead of being wired into a central switch.
var execHandlers = map[reflect.Type]execHandler{}

// registerExec associates a typed statement handler with its AST type.
// It is generic over the statement type T so callers can pass their existing
// strongly-typed methods (e.g. (*Executor).executeCreateTable) directly; the
// registry stores a small wrapper that performs the type assertion.
func registerExec[T ast.Statement](fn func(*Executor, context.Context, T) (*Result, error)) {
	var zero T // typed-nil pointer; yields the concrete *ast.X reflect.Type
	t := reflect.TypeOf(zero)
	if _, exists := execHandlers[t]; exists {
		panic("executor: duplicate handler registered for " + t.String())
	}
	execHandlers[t] = func(e *Executor, ctx context.Context, s ast.Statement) (*Result, error) {
		return fn(e, ctx, s.(T))
	}
}

// dispatch looks up and invokes the handler registered for stmt's concrete
// type. The bool reports whether a handler was found.
func (e *Executor) dispatch(ctx context.Context, stmt ast.Statement) (*Result, error, bool) {
	h, ok := execHandlers[reflect.TypeOf(stmt)]
	if !ok {
		return nil, nil, false
	}
	res, err := h(e, ctx, stmt)
	return res, err, true
}
