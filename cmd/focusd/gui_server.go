package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/parser"
)

//go:embed static
var staticFiles embed.FS

type apiQueryRequest struct {
	SQL string `json:"sql"`
}

type apiQueryResponse struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Tag     string          `json:"tag"`
	Error   string          `json:"error,omitempty"`
	ElapsedMs int64         `json:"elapsed_ms"`
}

type apiColumnInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"notNull"`
}

type apiTableInfo struct {
	Schema  string          `json:"schema"`
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	Columns []apiColumnInfo `json:"columns"`
}

type apiTriggerInfo struct {
	Name   string `json:"name"`
	Table  string `json:"table"`
	Timing string `json:"timing"`
	Event  string `json:"event"`
}

type apiJobInfo struct {
	Name     string `json:"name"`
	Interval int    `json:"interval"`
	Unit     string `json:"unit"`
	Enabled  bool   `json:"enabled"`
}

type apiProcInfo struct {
	Name   string `json:"name"`
	Params []string `json:"params"`
}

type apiObjectsResponse struct {
	Triggers   []apiTriggerInfo `json:"triggers"`
	Jobs       []apiJobInfo     `json:"jobs"`
	Procedures []apiProcInfo    `json:"procedures"`
}

type apiValidateResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

type diagramColDTO struct {
	Name string `json:"name"`
	Type string `json:"type"`
	IsPK bool   `json:"isPK"`
	IsFK bool   `json:"isFK"`
}

type diagramTableDTO struct {
	Name    string          `json:"name"`
	Columns []diagramColDTO `json:"columns"`
}

type diagramFKDTO struct {
	FromTable string `json:"fromTable"`
	FromCol   string `json:"fromCol"`
	ToTable   string `json:"toTable"`
	ToCol     string `json:"toCol"`
}

type diagramDTO struct {
	Tables []diagramTableDTO `json:"tables"`
	FKs    []diagramFKDTO    `json:"fks"`
}

func startGUIServer(addr string, h executeHandler, cat *catalog.Catalog) {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("focus: gui: failed to mount static files: %v", err)
		return
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req apiQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, apiQueryResponse{Error: "invalid request body"})
			return
		}
		if req.SQL == "" {
			writeJSON(w, apiQueryResponse{Error: "empty query"})
			return
		}

		start := time.Now()
		result, err := h.Handle(req.SQL)
		elapsed := time.Since(start).Milliseconds()

		if err != nil {
			writeJSON(w, apiQueryResponse{Error: err.Error(), ElapsedMs: elapsed})
			return
		}

		rows := sanitizeRows(result.Rows)
		writeJSON(w, apiQueryResponse{
			Columns:   result.Columns,
			Rows:      rows,
			Tag:       result.Tag,
			ElapsedMs: elapsed,
		})
	})

	mux.HandleFunc("/api/schema", func(w http.ResponseWriter, r *http.Request) {
		tableRows := cat.GetInformationSchemaTablesForDatabase("postgres")
		colRows := cat.GetInformationSchemaColumnsForDatabase("postgres")

		// Build column index: schema.table -> []apiColumnInfo
		colIndex := make(map[string][]apiColumnInfo)
		for _, row := range colRows {
			if len(row) < 6 {
				continue
			}
			schema := fmt.Sprintf("%v", row[1])
			tname := fmt.Sprintf("%v", row[2])
			key := schema + "." + tname
			colIndex[key] = append(colIndex[key], apiColumnInfo{
				Name: fmt.Sprintf("%v", row[3]),
				Type: fmt.Sprintf("%v", row[5]),
			})
		}

		tables := make([]apiTableInfo, 0, len(tableRows))
		for _, row := range tableRows {
			if len(row) < 4 {
				continue
			}
			schema := fmt.Sprintf("%v", row[1])
			name := fmt.Sprintf("%v", row[2])
			kind := fmt.Sprintf("%v", row[3])
			key := schema + "." + name
			tables = append(tables, apiTableInfo{
				Schema:  schema,
				Name:    name,
				Kind:    kind,
				Columns: colIndex[key],
			})
		}

		writeJSON(w, tables)
	})

	mux.HandleFunc("/api/objects", func(w http.ResponseWriter, r *http.Request) {
		triggers := cat.GetAllTriggers()
		jobs := cat.GetAllJobs()
		procs := cat.GetAllProcedures()

		resp := apiObjectsResponse{
			Triggers:   make([]apiTriggerInfo, 0, len(triggers)),
			Jobs:       make([]apiJobInfo, 0, len(jobs)),
			Procedures: make([]apiProcInfo, 0, len(procs)),
		}

		for _, t := range triggers {
			resp.Triggers = append(resp.Triggers, apiTriggerInfo{
				Name:   t.Name,
				Table:  t.Table,
				Timing: t.Timing,
				Event:  t.Event,
			})
		}
		for _, j := range jobs {
			resp.Jobs = append(resp.Jobs, apiJobInfo{
				Name:     j.Name,
				Interval: j.Interval,
				Unit:     j.Unit,
				Enabled:  j.Enabled,
			})
		}
		for _, p := range procs {
			params := make([]string, len(p.Parameters))
			for i, param := range p.Parameters {
				params[i] = param.Name.Name + " " + param.Type
			}
			resp.Procedures = append(resp.Procedures, apiProcInfo{
				Name:   p.Name,
				Params: params,
			})
		}

		writeJSON(w, resp)
	})

	mux.HandleFunc("/api/diagram", func(w http.ResponseWriter, r *http.Request) {
		tables := cat.GetTablesForDiagram("public")

		dto := diagramDTO{
			Tables: make([]diagramTableDTO, 0, len(tables)),
			FKs:    []diagramFKDTO{},
		}

		// Build PK set per table for quick lookup
		pkCols := make(map[string]map[string]bool) // table -> col -> isPK
		for _, t := range tables {
			pkCols[t.Name] = make(map[string]bool)
			for _, c := range t.Constraints {
				if c.Type == constants.ConstraintPrimaryKey {
					pkCols[t.Name][c.ColumnName] = true
				}
			}
		}

		// Build FK set per table
		fkCols := make(map[string]map[string]bool) // table -> col -> isFK
		for _, t := range tables {
			fkCols[t.Name] = make(map[string]bool)
			for _, c := range t.Constraints {
				if c.Type == constants.ConstraintForeignKey {
					fkCols[t.Name][c.ColumnName] = true
					dto.FKs = append(dto.FKs, diagramFKDTO{
						FromTable: t.Name,
						FromCol:   c.ColumnName,
						ToTable:   c.ReferencedTable,
						ToCol:     c.ReferencedCol,
					})
				}
			}
		}

		for _, t := range tables {
			cols := make([]diagramColDTO, 0, len(t.Columns))
			for _, col := range t.Columns {
				cols = append(cols, diagramColDTO{
					Name: col.Name,
					Type: col.Type,
					IsPK: pkCols[t.Name][col.Name],
					IsFK: fkCols[t.Name][col.Name],
				})
			}
			dto.Tables = append(dto.Tables, diagramTableDTO{
				Name:    t.Name,
				Columns: cols,
			})
		}

		writeJSON(w, dto)
	})

	mux.HandleFunc("/api/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req apiQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, apiValidateResponse{Valid: false, Error: "invalid request"})
			return
		}
		if req.SQL == "" {
			writeJSON(w, apiValidateResponse{Valid: true})
			return
		}

		p := parser.NewParser(req.SQL)
		for !p.AtEOF() {
			_, err := p.ParseStatement()
			if err != nil {
				writeJSON(w, apiValidateResponse{Valid: false, Error: err.Error()})
				return
			}
		}
		writeJSON(w, apiValidateResponse{Valid: true})
	})

	log.Printf("focus: GUI available at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("focus: GUI server error: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// sanitizeRows converts nil values and non-standard types to JSON-safe equivalents.
func sanitizeRows(rows [][]interface{}) [][]interface{} {
	if rows == nil {
		return [][]interface{}{}
	}
	out := make([][]interface{}, len(rows))
	for i, row := range rows {
		sanitized := make([]interface{}, len(row))
		for j, val := range row {
			switch v := val.(type) {
			case nil:
				sanitized[j] = nil
			case []byte:
				sanitized[j] = string(v)
			case int8, int16, int32, int64, uint8, uint16, uint32, uint64:
				sanitized[j] = fmt.Sprintf("%v", v)
			default:
				sanitized[j] = v
			}
		}
		out[i] = sanitized
	}
	return out
}
