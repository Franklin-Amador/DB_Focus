package catalog

import (
	"dbf/internal/ast"
	"dbf/internal/constants"
	"fmt"
	"sync/atomic"
)

// triggerOIDCounter generates unique OIDs for triggers
var triggerOIDCounter int64 = 32768 // Start above procedure OIDs

func (c *Catalog) CreateTrigger(name, timing, event, table string, forEachRow bool, body []ast.Statement) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if triggers, exists := c.triggers[table]; exists {
		for _, t := range triggers {
			if t.Name == name {
				return fmt.Errorf("trigger %s already exists on table %s", name, table)
			}
		}
	}

	trigger := &Trigger{
		Name:       name,
		Timing:     timing,
		Event:      event,
		Table:      table,
		ForEachRow: forEachRow,
		Body:       body,
	}

	c.triggers[table] = append(c.triggers[table], trigger)

	// Register in pg_catalog.pg_trigger
	if err := c.registerTriggerInCatalog(name, timing, event, table); err != nil {
		// Log but don't fail - trigger is still created in memory
		fmt.Printf("warning: failed to register trigger in catalog: %v\n", err)
	}

	return nil
}

// registerTriggerInCatalog adds trigger to pg_catalog.pg_trigger
// Caller must hold the write lock on c.mu
func (c *Catalog) registerTriggerInCatalog(name, timing, event, tableName string) error {
	triggerTable, err := c.getTableUnlocked(constants.CatalogTrigger)
	if err != nil {
		return err
	}

	oid := atomic.AddInt64(&triggerOIDCounter, 1)

	// Calculate tgtype bitmap: BEFORE=2, AFTER=0, INSERT=4, UPDATE=8, DELETE=16, ROW=1
	tgtype := 0
	if timing == constants.TriggerBefore {
		tgtype |= 2
	}
	switch event {
	case constants.TriggerInsert:
		tgtype |= 4
	case constants.TriggerUpdate:
		tgtype |= 8
	case constants.TriggerDelete:
		tgtype |= 16
	}
	tgtype |= 1 // FOR EACH ROW

	// oid, tgname, tgrelid, tgtype, tgenabled, tgisinternal, tgconstrrelid, tgfoid, tgargs
	row := []interface{}{
		int(oid),
		name,
		0, // tgrelid - would need table OID lookup
		tgtype,
		"O",   // enabled (Origin)
		false, // not internal
		0,     // no constraint relation
		0,     // no function (inline trigger)
		"",    // no args
	}

	return triggerTable.InsertRowUnsafe(row)
}

func (c *Catalog) GetTriggers(table, timing, event string) []*Trigger {
	c.mu.RLock()
	defer c.mu.RUnlock()

	triggers, exists := c.triggers[table]
	if !exists {
		return nil
	}

	result := make([]*Trigger, 0, len(triggers))
	for _, t := range triggers {
		if t.Timing == timing && t.Event == event {
			result = append(result, t)
		}
	}
	return result
}

func (c *Catalog) DropTrigger(name, table string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	triggers, exists := c.triggers[table]
	if !exists {
		return fmt.Errorf("no triggers found on table %s", table)
	}

	for i, t := range triggers {
		if t.Name == name {
			c.triggers[table] = append(triggers[:i], triggers[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("trigger %s not found on table %s", name, table)
}
