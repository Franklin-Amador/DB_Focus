package main

// gui_api.go: HTTP handlers + DTOs del API del GUI Studio.
// Los 5 endpoints históricos conservan su contrato (campos extra son aditivos).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbf/internal/catalog"
	"dbf/internal/constants"
	"dbf/internal/parser"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────────

type apiQueryRequest struct {
	SQL     string `json:"sql"`
	MaxRows int    `json:"maxRows,omitempty"`
	// Database is the GUI's active database (the statements run inside it).
	// Empty means the default database.
	Database string `json:"database,omitempty"`
	// Schema is the GUI's active schema: unqualified object names in the SQL
	// resolve inside it (like a session search_path). Empty means "public".
	Schema string `json:"schema,omitempty"`
}

// apiDatabaseInfo describes one database for the sidebar/selector.
type apiDatabaseInfo struct {
	Name      string `json:"name"`
	Schemas   int    `json:"schemas"`
	Tables    int    `json:"tables"`
	Views     int    `json:"views"`
	IsDefault bool   `json:"isDefault"`
}

// apiSchemaInfo describes one user-visible schema for the sidebar/selector.
type apiSchemaInfo struct {
	Name      string `json:"name"`
	Tables    int    `json:"tables"`
	Views     int    `json:"views"`
	IsDefault bool   `json:"isDefault"`
}

type apiQueryResponse struct {
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
	Tag       string          `json:"tag"`
	Error     string          `json:"error,omitempty"`
	ElapsedMs int64           `json:"elapsed_ms"`
	Truncated bool            `json:"truncated,omitempty"`
}

type apiScriptResponse struct {
	Results     []apiScriptStatement `json:"results"`
	FailedIndex int                  `json:"failedIndex"`
	FailedSQL   string               `json:"failedSql,omitempty"`
	Error       string               `json:"error,omitempty"`
}

type apiScriptStatement struct {
	Index     int             `json:"index"`
	SQL       string          `json:"sql"`
	Tag       string          `json:"tag"`
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
	ElapsedMs int64           `json:"elapsedMs"`
	Truncated bool            `json:"truncated,omitempty"`
}

type apiColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	NotNull  bool   `json:"notNull"`
	Identity bool   `json:"identity"`
	IsPK     bool   `json:"isPK"`
	IsFK     bool   `json:"isFK"`
	IsUnique bool   `json:"isUnique"`
	Ordinal  int    `json:"ordinal"`
}

type apiIndexInfo struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

type apiTableInfo struct {
	Schema         string          `json:"schema"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	RowCount       int             `json:"rowCount"`
	Columns        []apiColumnInfo `json:"columns"`
	Indexes        []apiIndexInfo  `json:"indexes,omitempty"`
	ViewDefinition string          `json:"viewDefinition,omitempty"`
}

type apiTriggerInfo struct {
	Name       string `json:"name"`
	Table      string `json:"table"`
	Timing     string `json:"timing"`
	Event      string `json:"event"`
	ForEachRow bool   `json:"forEachRow"`
	BodyText   string `json:"bodyText,omitempty"`
}

type apiJobInfo struct {
	Name     string `json:"name"`
	Interval int    `json:"interval"`
	Unit     string `json:"unit"`
	Enabled  bool   `json:"enabled"`
	LastRun  int64  `json:"lastRun"`
	NextRun  int64  `json:"nextRun"`
	BodyText string `json:"bodyText,omitempty"`
}

type apiProcInfo struct {
	Name     string   `json:"name"`
	Params   []string `json:"params"`
	BodyText string   `json:"bodyText,omitempty"`
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
	Name     string `json:"name"`
	Type     string `json:"type"`
	IsPK     bool   `json:"isPK"`
	IsFK     bool   `json:"isFK"`
	IsUnique bool   `json:"isUnique"`
	NotNull  bool   `json:"notNull"`
	Identity bool   `json:"identity"`
}

type diagramTableDTO struct {
	Name     string          `json:"name"`
	RowCount int             `json:"rowCount"`
	PK       []string        `json:"pk"`
	Columns  []diagramColDTO `json:"columns"`
	Indexes  []apiIndexInfo  `json:"indexes,omitempty"`
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

type apiTableDataResponse struct {
	Columns []apiColumnInfo `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	Total   int             `json:"total"`
	Offset  int             `json:"offset"`
	PK      []string        `json:"pk"`
	Error   string          `json:"error,omitempty"`
}

// ─── Helpers de metadata ──────────────────────────────────────────────────────

// tableMeta agrega la vista enriquecida de una tabla del catálogo.
type tableMeta struct {
	dt       catalog.DiagramTable
	pk       []string
	fkCols   map[string]bool
	uniqCols map[string]bool
	rowCount int
	indexes  []apiIndexInfo
}

// collectTableMeta reúne columnas+constraints (copia bajo RLock del catálogo)
// y rowCount+índices (bajo RLock de cada tabla), ordenado por nombre.
func collectTableMeta(cat *catalog.Catalog, schema string) []tableMeta {
	dts := cat.GetTablesForDiagram(schema)
	sort.Slice(dts, func(i, j int) bool { return dts[i].Name < dts[j].Name })

	out := make([]tableMeta, 0, len(dts))
	for _, dt := range dts {
		m := tableMeta{dt: dt, fkCols: map[string]bool{}, uniqCols: map[string]bool{}}
		for _, c := range dt.Constraints {
			switch c.Type {
			case constants.ConstraintPrimaryKey:
				m.pk = append(m.pk, c.ColumnName)
			case constants.ConstraintForeignKey:
				m.fkCols[c.ColumnName] = true
			case constants.ConstraintUnique:
				m.uniqCols[c.ColumnName] = true
			}
		}
		if t, err := cat.GetTable(dt.Name, schema); err == nil {
			m.rowCount = len(t.SelectAll())
			t.Mu().RLock()
			for _, idx := range t.Indexes {
				m.indexes = append(m.indexes, apiIndexInfo{
					Name:    idx.Name,
					Columns: append([]string(nil), idx.ColumnNames...),
				})
			}
			t.Mu().RUnlock()
			sort.Slice(m.indexes, func(i, j int) bool { return m.indexes[i].Name < m.indexes[j].Name })
		}
		out = append(out, m)
	}
	return out
}

func (m *tableMeta) isPK(col string) bool {
	for _, p := range m.pk {
		if p == col {
			return true
		}
	}
	return false
}

func (m *tableMeta) apiColumns() []apiColumnInfo {
	cols := make([]apiColumnInfo, 0, len(m.dt.Columns))
	for i, col := range m.dt.Columns {
		cols = append(cols, apiColumnInfo{
			Name:     col.Name,
			Type:     col.Type,
			NotNull:  col.NotNull,
			Identity: col.Identity,
			IsPK:     m.isPK(col.Name),
			IsFK:     m.fkCols[col.Name],
			IsUnique: m.uniqCols[col.Name],
			Ordinal:  i + 1,
		})
	}
	return cols
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func handleAPIQuery(h *executeHandler, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apiQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, apiQueryResponse{Error: "invalid request body"})
			return
		}
		if req.SQL == "" {
			writeJSON(w, apiQueryResponse{Error: "empty query"})
			return
		}

		ctx, cancel := queryContext(r, timeout)
		defer cancel()

		start := time.Now()
		result, err := h.HandleQueryCtx(ctx, req.SQL, req.Database, req.Schema)
		elapsed := time.Since(start).Milliseconds()

		if err != nil {
			writeJSON(w, apiQueryResponse{Error: err.Error(), ElapsedMs: elapsed})
			return
		}

		rows := sanitizeRows(result.Rows)
		truncated := false
		if req.MaxRows > 0 && len(rows) > req.MaxRows {
			rows = rows[:req.MaxRows]
			truncated = true
		}
		writeJSON(w, apiQueryResponse{
			Columns:   result.Columns,
			Rows:      rows,
			Tag:       result.Tag,
			ElapsedMs: elapsed,
			Truncated: truncated,
		})
	}
}

func handleAPIScript(h *executeHandler, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apiQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, apiScriptResponse{FailedIndex: 0, Error: "invalid request body"})
			return
		}
		if req.SQL == "" {
			writeJSON(w, apiScriptResponse{FailedIndex: -1, Results: []apiScriptStatement{}})
			return
		}

		ctx, cancel := queryContext(r, timeout)
		defer cancel()

		res := h.HandleScript(ctx, req.SQL, req.MaxRows, req.Database, req.Schema)

		resp := apiScriptResponse{
			Results:     make([]apiScriptStatement, 0, len(res.Results)),
			FailedIndex: res.FailedIndex,
			FailedSQL:   res.FailedSQL,
		}
		if res.Err != nil {
			resp.Error = res.Err.Error()
		}
		for _, sr := range res.Results {
			resp.Results = append(resp.Results, apiScriptStatement{
				Index:     sr.Index,
				SQL:       sr.SQL,
				Tag:       sr.Tag,
				Columns:   sr.Columns,
				Rows:      sanitizeRows(sr.Rows),
				ElapsedMs: sr.ElapsedMs,
				Truncated: sr.Truncated,
			})
		}
		writeJSON(w, resp)
	}
}

// handleAPIDatabases lists the databases of the cluster with object counts.
func handleAPIDatabases(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		infos := cl.ListDatabases()
		out := make([]apiDatabaseInfo, 0, len(infos))
		for _, d := range infos {
			out = append(out, apiDatabaseInfo{Name: d.Name, Schemas: d.Schemas, Tables: d.Tables, Views: d.Views, IsDefault: d.IsDefault})
		}
		writeJSON(w, out)
	}
}

// handleAPISchemas lists the user-visible schemas of a database with object counts.
func handleAPISchemas(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok := requestCatalog(w, r, cl)
		if !ok {
			return
		}
		infos := cat.ListSchemas()
		out := make([]apiSchemaInfo, 0, len(infos))
		for _, s := range infos {
			out = append(out, apiSchemaInfo{Name: s.Name, Tables: s.Tables, Views: s.Views, IsDefault: s.Name == "public"})
		}
		writeJSON(w, out)
	}
}

func handleAPISchema(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok := requestCatalog(w, r, cl)
		if !ok {
			return
		}
		schema, ok := requestSchema(w, r, cat)
		if !ok {
			return
		}
		metas := collectTableMeta(cat, schema)

		tables := make([]apiTableInfo, 0, len(metas)+4)
		for i := range metas {
			m := &metas[i]
			tables = append(tables, apiTableInfo{
				Schema:   m.dt.Schema,
				Name:     m.dt.Name,
				Kind:     "BASE TABLE",
				RowCount: m.rowCount,
				Columns:  m.apiColumns(),
				Indexes:  m.indexes,
			})
		}

		// Vistas: columnas propias + definición SQL original.
		views := cat.GetAllViewsInSchema(schema)
		viewNames := make([]string, 0, len(views))
		for name := range views {
			viewNames = append(viewNames, name)
		}
		sort.Strings(viewNames)
		for _, name := range viewNames {
			v := views[name]
			cols := make([]apiColumnInfo, 0, len(v.Columns))
			for i, c := range v.Columns {
				cols = append(cols, apiColumnInfo{Name: c.Name, Type: c.Type, Ordinal: i + 1})
			}
			tables = append(tables, apiTableInfo{
				Schema:         schema,
				Name:           name,
				Kind:           "VIEW",
				Columns:        cols,
				ViewDefinition: v.QueryText,
			})
		}

		writeJSON(w, tables)
	}
}

func handleAPIObjects(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok := requestCatalog(w, r, cl)
		if !ok {
			return
		}
		triggers := cat.GetAllTriggers()
		jobs := cat.GetAllJobs()
		procs := cat.GetAllProcedures()

		sort.Slice(triggers, func(i, j int) bool { return triggers[i].Name < triggers[j].Name })
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
		sort.Slice(procs, func(i, j int) bool { return procs[i].Name < procs[j].Name })

		resp := apiObjectsResponse{
			Triggers:   make([]apiTriggerInfo, 0, len(triggers)),
			Jobs:       make([]apiJobInfo, 0, len(jobs)),
			Procedures: make([]apiProcInfo, 0, len(procs)),
		}

		for _, t := range triggers {
			body := ""
			for _, tr := range cat.GetTriggers(t.Table, t.Timing, t.Event) {
				if tr.Name == t.Name {
					body = tr.BodyText
					break
				}
			}
			resp.Triggers = append(resp.Triggers, apiTriggerInfo{
				Name:       t.Name,
				Table:      t.Table,
				Timing:     t.Timing,
				Event:      t.Event,
				ForEachRow: t.ForEachRow,
				BodyText:   body,
			})
		}
		for _, j := range jobs {
			next := int64(0)
			if j.LastRun > 0 {
				next = j.LastRun + int64(jobIntervalSeconds(j.Interval, j.Unit))
			}
			resp.Jobs = append(resp.Jobs, apiJobInfo{
				Name:     j.Name,
				Interval: j.Interval,
				Unit:     j.Unit,
				Enabled:  j.Enabled,
				LastRun:  j.LastRun,
				NextRun:  next,
				BodyText: j.BodyText,
			})
		}
		for _, p := range procs {
			params := make([]string, len(p.Parameters))
			for i, param := range p.Parameters {
				params[i] = param.Name.Name + " " + param.Type
			}
			body := ""
			if proc, err := cat.GetProcedure(p.Name); err == nil {
				body = proc.BodyText
			}
			resp.Procedures = append(resp.Procedures, apiProcInfo{
				Name:     p.Name,
				Params:   params,
				BodyText: body,
			})
		}

		writeJSON(w, resp)
	}
}

func jobIntervalSeconds(interval int, unit string) int {
	switch strings.ToUpper(unit) {
	case "MINUTE":
		return interval * 60
	case "HOUR":
		return interval * 3600
	case "DAY":
		return interval * 86400
	}
	return interval
}

func handleAPIDiagram(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok := requestCatalog(w, r, cl)
		if !ok {
			return
		}
		schema, ok := requestSchema(w, r, cat)
		if !ok {
			return
		}
		metas := collectTableMeta(cat, schema)

		dto := diagramDTO{
			Tables: make([]diagramTableDTO, 0, len(metas)),
			FKs:    []diagramFKDTO{},
		}

		for i := range metas {
			m := &metas[i]
			for _, c := range m.dt.Constraints {
				if c.Type == constants.ConstraintForeignKey {
					dto.FKs = append(dto.FKs, diagramFKDTO{
						FromTable: m.dt.Name,
						FromCol:   c.ColumnName,
						ToTable:   c.ReferencedTable,
						ToCol:     c.ReferencedCol,
					})
				}
			}

			cols := make([]diagramColDTO, 0, len(m.dt.Columns))
			for _, col := range m.dt.Columns {
				cols = append(cols, diagramColDTO{
					Name:     col.Name,
					Type:     col.Type,
					IsPK:     m.isPK(col.Name),
					IsFK:     m.fkCols[col.Name],
					IsUnique: m.uniqCols[col.Name],
					NotNull:  col.NotNull,
					Identity: col.Identity,
				})
			}
			pk := m.pk
			if pk == nil {
				pk = []string{}
			}
			dto.Tables = append(dto.Tables, diagramTableDTO{
				Name:     m.dt.Name,
				RowCount: m.rowCount,
				PK:       pk,
				Columns:  cols,
				Indexes:  m.indexes,
			})
		}

		sort.Slice(dto.FKs, func(i, j int) bool {
			if dto.FKs[i].FromTable != dto.FKs[j].FromTable {
				return dto.FKs[i].FromTable < dto.FKs[j].FromTable
			}
			return dto.FKs[i].FromCol < dto.FKs[j].FromCol
		})

		writeJSON(w, dto)
	}
}

func handleAPIValidate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

// handleAPITableData sirve páginas de filas de una tabla para el explorador.
// Las escrituras del explorador NO pasan por aquí: van por /api/query para
// atravesar executor → triggers → persistencia.
func handleAPITableData(cl *catalog.Cluster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok := requestCatalog(w, r, cl)
		if !ok {
			return
		}
		name := r.URL.Query().Get("table")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, apiTableDataResponse{Error: "missing table parameter"})
			return
		}
		schema := r.URL.Query().Get("schema")
		if schema == "" {
			schema = "public"
		}
		if !cat.SchemaExists(schema) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, apiTableDataResponse{Error: "schema not found: " + schema})
			return
		}

		// Las vistas no tienen filas propias: el frontend usa /api/query.
		if views := cat.GetAllViewsInSchema(schema); views[name] != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, apiTableDataResponse{Error: "views are read-only; query them via /api/query"})
			return
		}

		t, err := cat.GetTable(name, schema)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, apiTableDataResponse{Error: "table not found: " + name})
			return
		}

		offset := parseIntParam(r, "offset", 0)
		limit := parseIntParam(r, "limit", 100)
		if limit <= 0 {
			limit = 100
		}
		if limit > 500 {
			limit = 500
		}
		if offset < 0 {
			offset = 0
		}

		rows := t.SelectAll() // copia bajo lock de la tabla
		total := len(rows)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		page := sanitizeRows(rows[offset:end])

		// Metadata de columnas + PK para edición.
		metas := collectTableMeta(cat, schema)
		var cols []apiColumnInfo
		pk := []string{}
		for i := range metas {
			if metas[i].dt.Name == name {
				cols = metas[i].apiColumns()
				if metas[i].pk != nil {
					pk = metas[i].pk
				}
				break
			}
		}

		writeJSON(w, apiTableDataResponse{
			Columns: cols,
			Rows:    page,
			Total:   total,
			Offset:  offset,
			PK:      pk,
		})
	}
}

// ─── Utilidades ───────────────────────────────────────────────────────────────

// requestCatalog reads the optional ?database= parameter (default database)
// and answers 404 when the database does not exist. ok=false means the
// response was already written.
func requestCatalog(w http.ResponseWriter, r *http.Request, cl *catalog.Cluster) (*catalog.Catalog, bool) {
	name := r.URL.Query().Get("database")
	cat, ok := cl.Database(name)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "database not found: " + name})
		return nil, false
	}
	return cat, true
}

// requestSchema reads the optional ?schema= parameter (default "public") and
// answers 404 when the schema does not exist. ok=false means the response was
// already written.
func requestSchema(w http.ResponseWriter, r *http.Request, cat *catalog.Catalog) (string, bool) {
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}
	if !cat.SchemaExists(schema) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "schema not found: " + schema})
		return "", false
	}
	return schema, true
}

// queryContext deriva el contexto de la request con timeout opcional.
func queryContext(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(r.Context(), timeout)
	}
	return context.WithCancel(r.Context())
}

func parseIntParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
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
