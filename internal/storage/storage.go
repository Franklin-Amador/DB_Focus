package storage

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"dbf/internal/catalog"
)

// Backend is the interface for storage implementations
type Backend interface {
	SaveTable(table *catalog.Table) error
	LoadTable(cat *catalog.Catalog, name string) error
	LoadAll(cat *catalog.Catalog) error
	Close() error
}

type TableData struct {
	Name        string           `json:"name"`
	Columns     []ColumnData     `json:"columns"`
	Constraints []ConstraintData `json:"constraints"`
	Rows        [][]interface{}  `json:"rows"`
}

type ColumnData struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	NotNull       bool   `json:"not_null"`
	Identity      bool   `json:"identity"`
	IdentityValue int    `json:"identity_value"`
}

type ConstraintData struct {
	Type            string `json:"type"`
	ColumnName      string `json:"column_name"`
	ReferencedTable string `json:"referenced_table,omitempty"`
	ReferencedCol   string `json:"referenced_col,omitempty"`
}

type Storage struct {
	dir string
	mu  sync.RWMutex
}

func New(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Storage{dir: dir}, nil
}

func (s *Storage) SaveTable(table *catalog.Table) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	td := TableData{
		Name:        table.Name,
		Columns:     make([]ColumnData, len(table.Columns)),
		Constraints: make([]ConstraintData, len(table.Constraints)),
		Rows:        table.SelectAll(),
	}

	for i, col := range table.Columns {
		td.Columns[i] = ColumnData{
			Name:          col.Name,
			Type:          col.Type,
			NotNull:       col.NotNull,
			Identity:      col.Identity,
			IdentityValue: col.IdentityValue,
		}
	}

	for i, constraint := range table.Constraints {
		td.Constraints[i] = ConstraintData{
			Type:            constraint.Type,
			ColumnName:      constraint.ColumnName,
			ReferencedTable: constraint.ReferencedTable,
			ReferencedCol:   constraint.ReferencedCol,
		}
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.dir, table.Name+".json")
	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		return err
	}

	return nil
}

func (s *Storage) LoadTable(cat *catalog.Catalog, name string) error {
	if strings.HasPrefix(name, "pg_catalog.") {
		return nil
	}

	s.mu.RLock()
	path := filepath.Join(s.dir, name+".json")
	s.mu.RUnlock()

	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var td TableData
	if err := json.Unmarshal(data, &td); err != nil {
		return err
	}

	cols := make([]catalog.Column, len(td.Columns))
	for i, col := range td.Columns {
		cols[i] = catalog.Column{Name: col.Name, Type: col.Type, NotNull: col.NotNull, Identity: col.Identity, IdentityValue: col.IdentityValue}
	}

	constraints := make([]catalog.Constraint, len(td.Constraints))
	for i, constraint := range td.Constraints {
		constraints[i] = catalog.Constraint{
			Type:            constraint.Type,
			ColumnName:      constraint.ColumnName,
			ReferencedTable: constraint.ReferencedTable,
			ReferencedCol:   constraint.ReferencedCol,
		}
	}

	if err := cat.CreateTable(td.Name, cols, constraints); err != nil {
		return err
	}

	table, err := cat.GetTable(td.Name)
	if err != nil {
		return err
	}

	for _, row := range td.Rows {
		if err := table.InsertRow(row, cat); err != nil {
			return err
		}
	}

	syncIdentityValues(table)

	return nil
}

func syncIdentityValues(table *catalog.Table) {
	table.Mu().Lock()
	defer table.Mu().Unlock()

	for colIdx, col := range table.Columns {
		if !col.Identity {
			continue
		}
		maxValue := col.IdentityValue
		for _, row := range table.Rows {
			if colIdx >= len(row) {
				continue
			}
			value := row[colIdx]
			if value == nil {
				continue
			}

			switch v := value.(type) {
			case int:
				if v > maxValue {
					maxValue = v
				}
			case int64:
				if int(v) > maxValue {
					maxValue = int(v)
				}
			case float64:
				if int(v) > maxValue {
					maxValue = int(v)
				}
			case json.Number:
				if parsed, err := v.Int64(); err == nil && int(parsed) > maxValue {
					maxValue = int(parsed)
				}
			case string:
				if parsed, err := strconv.Atoi(v); err == nil && parsed > maxValue {
					maxValue = parsed
				}
			}
		}

		// Backfill missing identity values
		for i, row := range table.Rows {
			if colIdx >= len(row) {
				continue
			}
			if row[colIdx] == nil {
				maxValue++
				table.Rows[i][colIdx] = maxValue
			}
		}

		if maxValue > table.Columns[colIdx].IdentityValue {
			table.Columns[colIdx].IdentityValue = maxValue
		}
	}
}

func (s *Storage) LoadAll(cat *catalog.Catalog) error {
	s.mu.RLock()
	files, err := ioutil.ReadDir(s.dir)
	s.mu.RUnlock()

	if err != nil {
		return err
	}

	// First pass: load all table structures
	tableData := make(map[string]*TableData)
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		tableName := file.Name()[:len(file.Name())-5]
		if strings.HasPrefix(tableName, "pg_catalog.") {
			continue
		}
		path := filepath.Join(s.dir, file.Name())

		data, err := ioutil.ReadFile(path)
		if err != nil {
			fmt.Printf("warning: failed to read table %s: %v\n", tableName, err)
			continue
		}

		var td TableData
		if err := json.Unmarshal(data, &td); err != nil {
			fmt.Printf("warning: failed to unmarshal table %s: %v\n", tableName, err)
			continue
		}
		if strings.HasPrefix(td.Name, "pg_catalog.") {
			continue
		}
		tableData[tableName] = &td

		cols := make([]catalog.Column, len(td.Columns))
		for i, col := range td.Columns {
			cols[i] = catalog.Column{Name: col.Name, Type: col.Type, NotNull: col.NotNull}
		}

		constraints := make([]catalog.Constraint, len(td.Constraints))
		for i, constraint := range td.Constraints {
			constraints[i] = catalog.Constraint{
				Type:            constraint.Type,
				ColumnName:      constraint.ColumnName,
				ReferencedTable: constraint.ReferencedTable,
				ReferencedCol:   constraint.ReferencedCol,
			}
		}

		if err := cat.CreateTable(td.Name, cols, constraints); err != nil {
			fmt.Printf("warning: failed to create table %s: %v\n", tableName, err)
		}
	}

	// Second pass: load all data (now all tables exist)
	for tableName, td := range tableData {
		table, err := cat.GetTable(tableName)
		if err != nil {
			fmt.Printf("warning: table %s not found during data load: %v\n", tableName, err)
			continue
		}

		for _, row := range td.Rows {
			if err := table.InsertRowUnsafe(row); err != nil {
				fmt.Printf("warning: failed to insert row into %s: %v\n", tableName, err)
			}
		}
	}

	return nil
}
