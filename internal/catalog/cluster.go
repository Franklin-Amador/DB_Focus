package catalog

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// DefaultDatabase is the database every server has and every connection lands
// on when it does not name one (psql's -d default). It can never be dropped.
const DefaultDatabase = "postgres"

// Cluster is the top level of the object hierarchy: a set of databases, each
// with its own Catalog (schemas, tables, views, procedures, triggers, jobs).
// A database is a hard boundary: a query runs inside one Catalog and cannot
// reach objects of another database, like PostgreSQL.
type Cluster struct {
	mu        sync.RWMutex
	dbs       map[string]*Catalog
	dropHooks []func(name string)
}

// DatabaseInfo summarizes a database for listings (pg_database, GUI).
type DatabaseInfo struct {
	Name      string
	Schemas   int
	Tables    int
	Views     int
	IsDefault bool
}

var databaseNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NewCluster creates a cluster holding only the default database.
func NewCluster() *Cluster {
	cl := &Cluster{dbs: make(map[string]*Catalog)}
	cl.dbs[DefaultDatabase] = newCatalog(DefaultDatabase, cl)
	return cl
}

// Default returns the default database's catalog.
func (cl *Cluster) Default() *Catalog {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.dbs[DefaultDatabase]
}

// Database returns the catalog of the named database. An empty name means the
// default database.
func (cl *Cluster) Database(name string) (*Catalog, bool) {
	if name == "" {
		name = DefaultDatabase
	}
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	c, ok := cl.dbs[name]
	return c, ok
}

// DatabaseExists reports whether a database exists in the cluster.
func (cl *Cluster) DatabaseExists(name string) bool {
	_, ok := cl.Database(name)
	return ok
}

// CreateDatabase adds an empty database (with its own public schema and
// system catalog) and returns its catalog.
func (cl *Cluster) CreateDatabase(name string) (*Catalog, error) {
	if !databaseNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid database name %q", name)
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if _, exists := cl.dbs[name]; exists {
		return nil, fmt.Errorf("database %s already exists", name)
	}
	c := newCatalog(name, cl)
	cl.dbs[name] = c
	return c, nil
}

// DropDatabase removes a database and everything inside it, then notifies
// the drop hooks (executors, schedulers). The default database is protected.
func (cl *Cluster) DropDatabase(name string) error {
	if name == DefaultDatabase {
		return fmt.Errorf("cannot drop database %s: it is the default database", name)
	}
	cl.mu.Lock()
	if _, exists := cl.dbs[name]; !exists {
		cl.mu.Unlock()
		return fmt.Errorf("database %s does not exist", name)
	}
	delete(cl.dbs, name)
	hooks := append([]func(string){}, cl.dropHooks...)
	cl.mu.Unlock()
	for _, h := range hooks {
		h(name)
	}
	return nil
}

// AddDropHook registers a function called (outside the cluster lock) after a
// database is dropped, whatever code path dropped it.
func (cl *Cluster) AddDropHook(fn func(name string)) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.dropHooks = append(cl.dropHooks, fn)
}

// DatabaseNames returns the database names, default first then sorted. It is
// the cheap listing (no object counts) for pg_database and bookkeeping.
func (cl *Cluster) DatabaseNames() []string {
	cl.mu.RLock()
	names := make([]string, 0, len(cl.dbs))
	for name := range cl.dbs {
		names = append(names, name)
	}
	cl.mu.RUnlock()
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == DefaultDatabase) != (names[j] == DefaultDatabase) {
			return names[i] == DefaultDatabase
		}
		return names[i] < names[j]
	})
	return names
}

// ListDatabases returns every database with object counts, default first
// and the rest sorted by name.
func (cl *Cluster) ListDatabases() []DatabaseInfo {
	cl.mu.RLock()
	cats := make([]*Catalog, 0, len(cl.dbs))
	for _, c := range cl.dbs {
		cats = append(cats, c)
	}
	cl.mu.RUnlock()

	out := make([]DatabaseInfo, 0, len(cats))
	for _, c := range cats {
		info := DatabaseInfo{Name: c.name, IsDefault: c.name == DefaultDatabase}
		for _, s := range c.ListSchemas() {
			info.Schemas++
			info.Tables += s.Tables
			info.Views += s.Views
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// RegisterUser records a wire-protocol login in the default database's
// internal user registry (focus.users).
func (cl *Cluster) RegisterUser(username string, superuser bool) error {
	return cl.Default().RegisterUser(username, superuser)
}

// HandleSystemQueryForDatabase answers a system-catalog query against the
// named database (default when unknown), with the default "public" schema as
// the session schema.
func (cl *Cluster) HandleSystemQueryForDatabase(query, database string) (*SystemResult, bool) {
	return cl.HandleSystemQueryForSession(query, database, "public")
}

// HandleSystemQueryForSession answers a system-catalog query inside database
// with schema as the session search_path. It lets the wire server work on the
// cluster directly.
func (cl *Cluster) HandleSystemQueryForSession(query, database, schema string) (*SystemResult, bool) {
	c, ok := cl.Database(database)
	if !ok {
		// Not handled: the query falls through to the executor path, which
		// reports "database does not exist" instead of answering from another
		// database.
		return nil, false
	}
	return c.HandleSystemQueryForSession(query, database, schema)
}

// Name returns the database this catalog belongs to.
func (c *Catalog) Name() string {
	if c.name == "" {
		return DefaultDatabase
	}
	return c.name
}

// Cluster returns the cluster owning this catalog (nil for a detached catalog).
func (c *Catalog) Cluster() *Cluster {
	return c.cluster
}
