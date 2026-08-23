# REFACTOR.md — Roadmap de modularización de FocusDB

Documento vivo. Objetivo: reducir la fricción de agregar features (hoy un statement
nuevo obliga a tocar 5-7 sitios y copiar ~30 líneas de boilerplate) sin cambiar el
comportamiento observable del motor.

Estado general: **Fases 1–5 completas ✅.** Roadmap de modularización terminado. Ver Work Log en
`AGENTS.md`.

> **Entorno**: compilar/probar con `CGO_ENABLED=0` (el toolchain C no está en el PATH de `go`;
> Pebble tiene fallback puro-Go). Comandos: `CGO_ENABLED=0 go build ./internal/...` y
> `CGO_ENABLED=0 go test ./internal/...`.

---

## El problema de fondo

La arquitectura por capas es correcta (`parser → validator → executor → catalog → storage`,
sin ciclos de import). El problema no es la separación de paquetes: es que la asociación
**"keyword → nodo AST → handler → persistencia"** está codificada como control-flow repetido
en vez de como datos/abstracciones reutilizables.

Para agregar un statement (ej. `TRUNCATE`) hoy se tocan:

1. `token.go` — constante `TokenTruncate`
2. `token.go` — mapa `keywords`
3. `parser.go` — `case` en `ParseStatement()` **+** el string de error del `default` (ya desincronizado)
4. `parser.go` — función `parseTruncate()`
5. `ast.go` — nodo nuevo
6. `executor.go` — `case` en el type-switch
7. `executor_*.go` — handler que recopia ctx-check + resolución de schema + mutación + persistencia + `Result{Tag}`
8. `constants` — el tag
9. Si persiste: extender `storage.Backend` (18 métodos) **y sus 2 implementaciones**

---

## Diagnóstico por severidad

### 🔴 1. Boilerplate copiado en cada handler del executor
- Resolución de schema `schema := stmt.Table.Alias; if schema == "" { schema = "public" }`
  repetida en ~11 handlers, con **default inconsistente** (`""` vs `"public"`).
- Bloque `if e.storage != nil { GetTable + SaveTableWithSchema + fmt.Printf("warning...") }`
  copiado en ~13 sitios.
- **Manejo de error inconsistente**: al fallar `Save`, la mayoría imprime warning y devuelve
  *éxito* (disco y memoria divergen); los ALTER COLUMN devuelven error. Nada fuerza a persistir.
- ctx-check duplicado en el dispatcher y en casi cada handler, con dos estilos
  (`select { case <-ctx.Done() }` vs `ctx.Err()`).

### 🔴 2. Capa pg/system-catalog **triplicada** y frágil
La misma lógica vive en tres lugares que pueden divergir:
- `server/bypass.go` — intercepta por `strings.Contains`.
- `catalog/system_handler.go` (1023 líneas, ~400 comentadas/muertas) — switch gigante de
  `strings.Contains(upper, "PG_CATALOG.PG_DATABASE")` con respuestas hardcodeadas fila a fila.
- `catalog/system_catalog.go` — las mismas tablas creadas como tablas reales.

### 🔴 3. `storage.Backend`: interfaz de 18 métodos con implementación fantasma
`storage.go` mezcla persistencia de tablas/views/procs/triggers/jobs/schemas **y** mutación de
datos de columnas (`DropColumnData`, `RenameColumnData`). La impl. legacy `Storage` tiene 8
métodos que son stubs que retornan `nil` fingiendo cumplir el contrato.

### 🟡 4. `Catalog` es un god object
Un struct con `tables/views/procedures/triggers/jobs` bajo **un solo `RWMutex`**, con métodos
en 9 archivos, que además hace de motor de grafo de FKs y registro de usuarios.

### 🟡 5. Nodos AST anémicos — el techo real de features
El patrón (marker interfaces) es bueno; la expresividad no:
- `WhereClause` = un solo `Column = Value`: sin AND/OR, sin operadores `<`/`>`/`LIKE`, sin expresiones.
- `Insert.Values` = una fila; `Update` = una columna; `JoinClause` = una igualdad.
- Agregados por `strings.HasPrefix(..., "COUNT(")`: no hay SUM/AVG/MIN/MAX ni HAVING.
- `catalog → ast` fuerza `gob.Register` de nodos AST → cambiar un nodo rompe datos en disco.

### 🟡 6. `validator` desconectado y con lógica triplicada
Solo lo usa el executor (el parser no valida). Existencia de columnas chequeada en validator +
`catalog/table.go` + executor. Tres listas de tipos de datos que pueden divergir.

### 🟢 7. Otros focos internos
- `parseSelectItem` (~150 líneas) con 6 flags booleanos e inferencia de alias por heurística.
- Rutas JOIN vs no-JOIN duplican DISTINCT+ORDER BY+LIMIT en 4 lugares; 4 JOINs O(n·m) casi idénticos.
- Password hardcodeado `"4444"` en `server/conn.go`.

---

## Plan por fases

Ordenado por (impacto en facilidad de features) ÷ (riesgo). Las primeras 3 son bajo riesgo.

### Fase 1 — Matar el boilerplate  ✅ (núcleo hecho)
- [x] Helpers en executor (`executor_helpers.go`): `checkCtx`, `schemaOrPublic`, `qualifiedName`,
  `persistTable`, `persistTableWarn`. Reemplazados los ~13 ctx-guards, ~11 resoluciones de schema y
  el bloque de persistencia de tablas. Comportamiento y mensajes preservados.
- [x] Helpers en parser (`helpers.go`): `parseOptionalIfExists`, `parseOptionalCascadeRestrict` en
  los tres `parseDrop*`.
- [x] `parseBeginEndBlock()` para procedure/trigger/job; barrido de `parseIdentRequired` en
  drop index/procedure/trigger/job, alter job, create index.
- **Criterio de éxito:** verde con `CGO_ENABLED=0 go build/test ./internal/...` + tests de
  integración; cero cambio de comportamiento. ✅

### Fase 2 — Registry de statements  ✅
- [x] **Executor** (`executor_registry.go`): `registerExec[T]` genérico + `execHandlers`
  (`map[reflect.Type]execHandler`). `Execute` ahora hace lookup+dispatch; el type-switch de ~30
  casos desapareció. Cada handler se **auto-registra** en un `init()` dentro de su propio
  `executor_*.go` → agregar un statement ya **no obliga a editar `executor.go`**.
- [x] **Parser** (`parser_registry.go`): `topLevelParsers`, `createParsers`, `dropParsers`,
  `alterParsers` (`map[TokenType]stmtParseFn`, poblados en `init()` para evitar el ciclo de
  inicialización estático). `ParseStatement`/`parseCreate`/`parseDrop`/`parseAlter` colapsados a
  lookup + mensaje de error como único fallback.
- **Verificación:** `go build/vet/test ./internal/...` ✅; 20/20 tests de integración del motor ✅;
  arranque de servidor + `test-client` (protocolo extendido `SELECT 1`) ✅.
- **Nota:** de paso se corrigió un test obsoleto (`test-persistence-integration` usaba
  `DROP SCHEMA` sin CASCADE sobre un schema no vacío; fallaba ya en el baseline).

### Fase 3 — Consolidar capa pg/system-catalog  ✅
- [x] **Borradas ~400 líneas muertas** en `catalog/system_handler.go` (versión antigua comentada
  completa). Archivo: 1023 → 622 líneas. Verificado: build/vet/test + 4 tests cliente wire-protocol
  (`test-client`, `test-information-schema`, `test-users`, `test-simple-query`).
- [x] **Reachability documentada**: `checkBypass` (`server/bypass.go`) corre SIEMPRE antes de
  `HandleSystemQueryForDatabase` (`conn.go:155` antes de `:161`), y el path extendido
  (`conn.go:281`) llama `checkBypass` y luego `executeQuery` — nunca `HandleSystemQuery`. Por tanto
  los casos inline de `system_handler` que también matchea `bypass` (`pg_is_in_recovery`,
  `pg_replication_slots`, `extname='bdr'`, `pg_stat_gssapi`, `WHERE 1<>1`) están **sombreados**
  (inalcanzables) — son duplicación latente, no divergencia activa.
- [x] **Switch → tabla de rutas.** `HandleSystemQueryForDatabase` (el switch de ~22 casos de
  `strings.Contains`) reescrito como una tabla ordenada `systemRoutes []systemRoute{match, handle}`
  (primer match gana; orden y predicados idénticos → transformación 1:1). Helpers `hasAny` y
  `canned`. Agregar una system-query ahora es una entrada en la tabla. Verificado: build/vet/test +
  tests cliente wire-protocol (`test-client`, `test-information-schema`, `test-users`,
  `test-simple-query`, `test-multi-client`) ✅.
- **Layering documentado**: `server/bypass.go` (ya es una tabla limpia) corre SIEMPRE antes de
  `HandleSystemQueryForDatabase`, así que las entradas solapadas de `systemRoutes` (pg_is_in_recovery,
  pg_replication_slots, extname='bdr', pg_stat_gssapi, WHERE 1<>1) actúan como fallback defensivo.
  No se fusionaron entre paquetes (bypass escribe wire directo en `server`; system_handler construye
  `SystemResult` en `catalog`) para no arriesgar compatibilidad con clientes GUI que este entorno no
  puede verificar del todo — pero ambas capas son ahora tablas patrón→respuesta explícitas.

### Fase 4 — Segregar `storage.Backend` y `Catalog`  ✅ (núcleo hecho)
- [x] **Borrada la implementación legacy `Storage`** (backend JSON toda-stubs, nunca instanciada):
  `storage.go` 417 → 141 líneas. Se preservaron los helpers compartidos con el backend Pebble
  (`indexColumnsFromData`, `syncIdentityValues`).
- [x] **`Backend` segregada** en interfaces por responsabilidad: `TableStore`, `ViewStore`,
  `ProcedureStore`, `TriggerStore`, `JobStore`, `SchemaStore`, compuestas en `Backend`
  (+ `LoadAll`/`Close`). `PebbleStorage` y el executor quedan igual (cero cambio de comportamiento).
- [ ] Resta (más invasivo, menor payoff): mover la mutación de filas de `DropColumnData`/
  `RenameColumnData` al catálogo; sub-catálogos con locks propios (`Catalog` god object).
- **Verificación:** build/vet/test ✅; 20/20 integración del motor (incluye ciclos de
  persistencia+reload) ✅.

### Fase 5 — AST expresivo  ⏳ EN PROGRESO
- [x] **Paso 1: operadores de comparación en WHERE** (`=`, `<>`/`!=`, `<`, `>`, `<=`, `>=`) en
  SELECT/UPDATE/DELETE. Cambios: `token.go` (`TokenLte`/`TokenGte`), `lexer.go` (`>`, `>=`, `<=`,
  `!=`), `ast.WhereClause.Operator`, `parseWhereClause` + `whereOperator`, comparador type-aware
  `executor/where.go` (`whereMatches`/`compareCells`), y los 4 sitios de evaluación (fetchRows con
  índice preservado para `=`, filterJoinedRows, UPDATE, DELETE). Test: `cmd/test-where-operators`.
- [x] **Paso 2: predicados compuestos** `AND`/`OR`. `WhereClause` ahora es un árbol (leaf =
  comparación; nodo = `Left Conj Right`). Parser con precedencia (OR<AND<comparación) + paréntesis
  (`parseWhereOr`/`parseWhereAnd`/`parseWherePredicate`). Evaluador recursivo `evalWhere` con
  short-circuit, usado en los 4 sitios (SELECT plano, JOIN, UPDATE, DELETE). Validator recorre el
  árbol (`WhereClause.LeafColumns` + `validateWhereColumns`). Token `AND` agregado. Fast-path de
  índice preservado solo para leaf `=`. Test: `cmd/test-where-operators` (16 asserts, incl. JOIN).
- [x] **Paso 3: más agregados** SUM/AVG/MIN/MAX (además de COUNT). Detección genérica de funciones
  de agregado en parser (`isAggregateFunc`) y executor (`parseAggregate`); cómputo unificado en
  `executor/aggregate.go` (`computeAggregate`/`toFloat`/`numberValue`), aplicado en scalar y GROUP BY.
  Fix: el fast-path de `SELECT func()` sin args ya no captura agregados con argumentos.
  Test: `cmd/test-aggregates` (12 asserts). SUM/AVG numéricos; MIN/MAX numérico o lexicográfico.
- [x] **Paso 4 (vistas): desacoplar persistencia del AST.** Las vistas ahora guardan el texto SQL
  original del SELECT (`ast.CreateView.QueryText` capturado con `Token.Pos`+`sliceFrom`;
  `catalog.View.QueryText`; `storage.ViewData.QueryText`) y al cargar se **re-parsea con el parser
  actual** (`parseViewQuery`), con fallback al AST `gob` para datos viejos. Así cambiar la forma de
  los nodos AST no invalida vistas persistidas. Test: `cmd/test-view-persistence` (round-trip).
- [x] **Paso 4 (resto): procedures/triggers/jobs.** Mismo patrón: `parseBeginEndBlock` ahora
  devuelve el texto del cuerpo (`Token.Pos`+`sliceFrom`), propagado a `ast.Create{Procedure,Trigger,
  Job}.BodyText` → `catalog.{Procedure,Trigger,Job}.BodyText` → `storage.{Procedure,Trigger,Job}Data.
  BodyText`. En `LoadAll` se re-parsea con `parseBody` (fallback al AST `gob`). Cubre el path
  dollar-quoted de procedures. Test: `cmd/test-routine-persistence` (proc+trigger re-parseados
  ejecutan/disparan tras reload). **Fase 5 completa.**
