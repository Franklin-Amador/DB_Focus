package catalog

import (
	"dbf/internal/ast"
	"fmt"
)

func (c *Catalog) CreateView(name string, columns []Column, query *ast.Select, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	viewName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		viewName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	if _, ok := c.tables[schema]; !ok {
		c.tables[schema] = make(map[string]*Table)
	}
	if _, ok := c.views[schema]; !ok {
		c.views[schema] = make(map[string]*View)
	}
	if _, exists := c.tables[schema][viewName]; exists {
		return fmt.Errorf("object %s.%s already exists as table", schema, viewName)
	}
	if _, exists := c.views[schema][viewName]; exists {
		return fmt.Errorf("view %s.%s already exists", schema, viewName)
	}

	c.views[schema][viewName] = &View{Name: viewName, Columns: columns, Query: query}
	return nil
}

func (c *Catalog) GetView(name string, schemaOpt ...string) (*View, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	schema := "public"
	viewName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		viewName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	viewsInSchema, ok := c.views[schema]
	if !ok {
		return nil, fmt.Errorf("schema %s does not exist", schema)
	}
	view, exists := viewsInSchema[viewName]
	if !exists {
		return nil, fmt.Errorf("view %s.%s not found", schema, viewName)
	}

	copyCols := append([]Column(nil), view.Columns...)
	return &View{Name: view.Name, Columns: copyCols, Query: view.Query}, nil
}

func (c *Catalog) DropView(name string, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	viewName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		viewName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	viewsInSchema, ok := c.views[schema]
	if !ok {
		return fmt.Errorf("schema %s does not exist", schema)
	}
	if _, exists := viewsInSchema[viewName]; !exists {
		return fmt.Errorf("view %s.%s does not exist", schema, viewName)
	}

	delete(viewsInSchema, viewName)
	return nil
}

func (c *Catalog) LoadView(name string, columns []Column, query *ast.Select, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	viewName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		viewName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	if _, ok := c.views[schema]; !ok {
		c.views[schema] = make(map[string]*View)
	}
	if _, exists := c.views[schema][viewName]; exists {
		return nil
	}
	c.views[schema][viewName] = &View{Name: viewName, Columns: append([]Column(nil), columns...), Query: query}
	return nil
}

func (c *Catalog) GetAllViewsInSchema(schema string) map[string]*View {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}
	viewsInSchema, ok := c.views[schema]
	if !ok {
		return map[string]*View{}
	}

	result := make(map[string]*View, len(viewsInSchema))
	for name, v := range viewsInSchema {
		result[name] = &View{Name: v.Name, Columns: append([]Column(nil), v.Columns...), Query: v.Query}
	}
	return result
}

// FindDependentViews returns names of views that depend on the given view
func (c *Catalog) FindDependentViews(viewName, schema string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}

	var dependent []string
	// Check all schemas for dependent views
	for schemaName, viewsInSchema := range c.views {
		for otherViewName, otherView := range viewsInSchema {
			if otherView.Query != nil && otherView.Query.Table.Name == viewName {
				dependent = append(dependent, fmt.Sprintf("%s.%s", schemaName, otherViewName))
			}
		}
	}
	return dependent
}

// FindViewsUsingSource returns qualified view names that reference sourceName in FROM/JOIN.
func (c *Catalog) FindViewsUsingSource(sourceName, schema string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}

	targetQualified := fmt.Sprintf("%s.%s", schema, sourceName)
	dependent := make([]string, 0)

	for schemaName, viewsInSchema := range c.views {
		for viewName, view := range viewsInSchema {
			if view.Query == nil {
				continue
			}

			fromName := view.Query.Table.Name
			fromQualified := fromName
			if view.Query.Table.Alias != "" {
				fromQualified = fmt.Sprintf("%s.%s", view.Query.Table.Alias, fromName)
			}

			joinName := ""
			joinQualified := ""
			if view.Query.Join != nil {
				joinName = view.Query.Join.Table.Name
				joinQualified = joinName
				if view.Query.Join.Table.Alias != "" {
					joinQualified = fmt.Sprintf("%s.%s", view.Query.Join.Table.Alias, joinName)
				}
			}

			if fromName == sourceName || fromQualified == targetQualified || joinName == sourceName || joinQualified == targetQualified {
				dependent = append(dependent, fmt.Sprintf("%s.%s", schemaName, viewName))
			}
		}
	}

	return dependent
}

// DropViewWithBehavior drops a view with CASCADE or RESTRICT behavior
// behavior: "CASCADE" (drop dependent views), "RESTRICT" (error if dependencies exist), "" (default = RESTRICT)
func (c *Catalog) DropViewWithBehavior(name, behavior string, schemaOpt ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	schema := "public"
	viewName := name
	if parts := splitQualifiedName(name); len(parts) == 2 {
		schema = parts[0]
		viewName = parts[1]
	} else if len(schemaOpt) > 0 && schemaOpt[0] != "" {
		schema = schemaOpt[0]
	}

	viewsInSchema, ok := c.views[schema]
	if !ok {
		return fmt.Errorf("schema %s does not exist", schema)
	}
	if _, exists := viewsInSchema[viewName]; !exists {
		return fmt.Errorf("view %s.%s does not exist", schema, viewName)
	}

	// Find dependent views
	var dependent []string
	for schemaName, views := range c.views {
		for otherViewName, view := range views {
			if view.Query != nil && view.Query.Table.Name == viewName {
				dependent = append(dependent, fmt.Sprintf("%s.%s", schemaName, otherViewName))
			}
		}
	}

	// Handle dependencies based on behavior
	if len(dependent) > 0 && behavior != "CASCADE" {
		return fmt.Errorf("cannot drop view %s.%s because other views depend on it: %v",
			schema, viewName, dependent)
	}

	// Drop the view
	delete(viewsInSchema, viewName)

	// If CASCADE, drop dependent views
	if behavior == "CASCADE" {
		for _, depName := range dependent {
			parts := splitQualifiedName(depName)
			depSchema := parts[0]
			depViewName := parts[1]
			if depSchema == "" {
				depSchema = "public"
			}
			delete(c.views[depSchema], depViewName)
		}
	}

	return nil
}
