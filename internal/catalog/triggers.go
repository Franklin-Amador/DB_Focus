package catalog

import (
	"dbf/internal/constants"
	"fmt"
	"sync/atomic"
)

// triggerOIDCounter generates unique OIDs for triggers
var triggerOIDCounter int64 = 32768 // Start above procedure OIDs

// CreateTrigger registers a new trigger (and a pg_catalog.pg_trigger row).
// normalizeTrigger keeps Event / Events consistent for both the single- and
// multi-event forms.
func (c *Catalog) CreateTrigger(t *Trigger) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if triggers, exists := c.triggers[t.Table]; exists {
		for _, existing := range triggers {
			if existing.Name == t.Name {
				return fmt.Errorf("trigger %s already exists on table %s", t.Name, t.Table)
			}
		}
	}

	normalizeTrigger(t)
	c.triggers[t.Table] = append(c.triggers[t.Table], t)

	// Register in pg_catalog.pg_trigger
	if err := c.registerTriggerInCatalog(t.Name, t.Timing, t.Events, t.Table); err != nil {
		// Log but don't fail - trigger is still created in memory
		fmt.Printf("warning: failed to register trigger in catalog: %v\n", err)
	}

	return nil
}

// normalizeTrigger keeps the single Event field and the Events slice in sync,
// tolerating data created through either form (or gob-decoded before Events).
func normalizeTrigger(t *Trigger) {
	if len(t.Events) == 0 && t.Event != "" {
		t.Events = []string{t.Event}
	}
	if t.Event == "" && len(t.Events) > 0 {
		t.Event = t.Events[0]
	}
}

// registerTriggerInCatalog adds trigger to pg_catalog.pg_trigger
// Caller must hold the write lock on c.mu
func (c *Catalog) registerTriggerInCatalog(name, timing string, events []string, tableName string) error {
	triggerTable, err := c.getTableUnlocked(constants.CatalogTrigger)
	if err != nil {
		return err
	}

	oid := atomic.AddInt64(&triggerOIDCounter, 1)

	// Calculate tgtype bitmap: BEFORE=2, AFTER=0, INSERT=4, UPDATE=8, DELETE=16, ROW=1.
	// A multi-event trigger ORs the event bits into a single row.
	tgtype := 0
	if timing == constants.TriggerBefore {
		tgtype |= 2
	}
	for _, event := range events {
		switch event {
		case constants.TriggerInsert:
			tgtype |= 4
		case constants.TriggerUpdate:
			tgtype |= 8
		case constants.TriggerDelete:
			tgtype |= 16
		}
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
		if t.Timing == timing && t.MatchesEvent(event) {
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

// LoadTrigger loads a persisted trigger into the catalog (used on restart).
// Unlike CreateTrigger, this does not re-register in pg_catalog.
func (c *Catalog) LoadTrigger(t *Trigger) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if trigger already exists
	if triggers, exists := c.triggers[t.Table]; exists {
		for _, existing := range triggers {
			if existing.Name == t.Name {
				return nil // Already loaded
			}
		}
	}

	normalizeTrigger(t)
	c.triggers[t.Table] = append(c.triggers[t.Table], t)
	return nil
}
