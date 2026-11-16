package catalog

import (
	"dbf/internal/ast"
	"fmt"
	"sort"
	"strings"
)

// functionKey builds the map key for a function from its schema and name.
func functionKey(schema, name string) string {
	if schema == "" {
		schema = "public"
	}
	return schema + "." + name
}

// resolveFunctionName splits a (possibly qualified) function name and an
// optional default schema into (schema, name).
func resolveFunctionName(name string, schemaOpt ...string) (string, string) {
	schema := "public"
	fnName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema, fnName = parts[0], parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}
	return schema, fnName
}

// CreateFunction registers a function. replace controls CREATE OR REPLACE
// semantics; ifNotExists makes an existing function a no-op success.
func (c *Catalog) CreateFunction(fn *Function, replace, ifNotExists bool) error {
	if err := checkIdentifier("function", fn.Name); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if fn.Schema == "" {
		fn.Schema = "public"
	}
	key := functionKey(fn.Schema, fn.Name)
	if _, exists := c.functions[key]; exists {
		if ifNotExists {
			return nil
		}
		if !replace {
			return fmt.Errorf("function %s already exists", key)
		}
	}
	if fn.Query != nil {
		ast.ApplyDefaultSchemaToSelect(fn.Query, fn.Schema)
	}
	c.functions[key] = fn
	return nil
}

// GetFunction returns a copy-safe reference to a function, or an error.
func (c *Catalog) GetFunction(name string, schemaOpt ...string) (*Function, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	schema, fnName := resolveFunctionName(name, schemaOpt...)
	fn, ok := c.functions[functionKey(schema, fnName)]
	if !ok {
		return nil, fmt.Errorf("function %s.%s not found", schema, fnName)
	}
	return fn, nil
}

// LoadFunction installs a function read from storage (idempotent on restart).
func (c *Catalog) LoadFunction(fn *Function) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if fn.Schema == "" {
		fn.Schema = "public"
	}
	key := functionKey(fn.Schema, fn.Name)
	if _, exists := c.functions[key]; exists {
		return nil
	}
	if fn.Query != nil {
		ast.ApplyDefaultSchemaToSelect(fn.Query, fn.Schema)
	}
	c.functions[key] = fn
	return nil
}

// DropFunction removes a function; ifExists makes a missing one a no-op.
func (c *Catalog) DropFunction(name string, ifExists bool, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema, fnName := resolveFunctionName(name, schemaOpt...)
	key := functionKey(schema, fnName)
	if _, exists := c.functions[key]; !exists {
		if ifExists {
			return nil
		}
		return fmt.Errorf("function %s.%s does not exist", schema, fnName)
	}
	delete(c.functions, key)
	return nil
}

// ListFunctions returns the functions of a schema, sorted by name.
func (c *Catalog) ListFunctions(schema string) []*Function {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}
	var out []*Function
	for _, fn := range c.functions {
		if fn.Schema == schema {
			out = append(out, fn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AllFunctions returns every function across schemas.
func (c *Catalog) AllFunctions() []*Function {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*Function, 0, len(c.functions))
	for _, fn := range c.functions {
		out = append(out, fn)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Signature renders the function's argument types as "(t1, t2)" for display
// (pg_proc / \df style listings).
func (fn *Function) Signature() string {
	parts := make([]string, 0, len(fn.Parameters))
	for _, p := range fn.Parameters {
		parts = append(parts, p.Type)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
