package catalog

import (
	"dbf/internal/ast"
	"dbf/internal/constants"
	"fmt"
	"strings"
	"sync/atomic"
)

// procOIDCounter generates unique OIDs for procedures
var procOIDCounter int64 = 16384 // Start above system OIDs

func (c *Catalog) CreateProcedure(name string, parameters []ast.Parameter, body []ast.Statement) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.procedures[name]; exists {
		return fmt.Errorf("procedure %s already exists", name)
	}

	c.procedures[name] = &Procedure{
		Name:       name,
		Parameters: parameters,
		Body:       body,
	}

	// Register in pg_catalog.pg_proc
	if err := c.registerProcedureInCatalog(name, parameters); err != nil {
		// Log but don't fail - procedure is still created in memory
		fmt.Printf("warning: failed to register procedure in catalog: %v\n", err)
	}

	return nil
}

// registerProcedureInCatalog adds procedure to pg_catalog.pg_proc
// Caller must hold the write lock on c.mu
func (c *Catalog) registerProcedureInCatalog(name string, parameters []ast.Parameter) error {
	procTable, err := c.getTableUnlocked(constants.CatalogProc)
	if err != nil {
		return err
	}

	oid := atomic.AddInt64(&procOIDCounter, 1)

	// Build argument types string
	argTypes := make([]string, len(parameters))
	for i, p := range parameters {
		argTypes[i] = p.Type
	}

	// oid, proname, pronamespace(public=2200), proowner, prolang, pronargs, proargtypes, prosrc
	row := []interface{}{
		int(oid),
		name,
		2200, // public namespace
		constants.DefaultOwner,
		0,                           // internal language
		len(parameters),             // number of arguments
		strings.Join(argTypes, ","), // argument types
		"[PROCEDURE BODY]",          // source placeholder
	}

	return procTable.InsertRowUnsafe(row)
}

func (c *Catalog) GetProcedure(name string) (*Procedure, error) {
	c.mu.RLock()
	proc, exists := c.procedures[name]
	if !exists {
		c.mu.RUnlock()
		return nil, fmt.Errorf("procedure %s not found", name)
	}

	procCopy := &Procedure{
		Name:       proc.Name,
		Parameters: append([]ast.Parameter(nil), proc.Parameters...),
		Body:       append([]ast.Statement(nil), proc.Body...),
	}
	c.mu.RUnlock()

	return procCopy, nil
}

// LoadProcedure inserts a procedure into memory without registering it again in pg_catalog.
// This is intended for persistence reload paths.
func (c *Catalog) LoadProcedure(name string, parameters []ast.Parameter, body []ast.Statement) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.procedures[name]; exists {
		return nil
	}

	c.procedures[name] = &Procedure{
		Name:       name,
		Parameters: parameters,
		Body:       body,
	}

	return nil
}

func (c *Catalog) DropProcedure(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.procedures[name]; !exists {
		return fmt.Errorf("procedure %s not found", name)
	}

	delete(c.procedures, name)
	return nil
}
