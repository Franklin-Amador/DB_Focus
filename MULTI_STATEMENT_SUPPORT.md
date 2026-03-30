# Multi-Statement Execution Support

## Objetivo
Habilitar la capacidad del gestor FocusDB para procesar múltiples consultas SQL separadas por `;` en una sola solicitud, lo cual es crítico para la compatibilidad con pgAdmin y otros clientes PostgreSQL.

## Cambios Implementados

### 1. Parser (`internal/parser/parser.go`)
- **Nuevo método `AtEOF()`**: Permite verificar si el parser ha llegado al final del input.
  ```go
  func (p *Parser) AtEOF() bool {
      return p.cur.Type == TokenEOF
  }
  ```

### 2. Executor (`internal/executor/executor_select.go`)
- **Actualización de `executeSelectFunction()`**: Añadido soporte para la función `version()`.
  ```go
  case "version":
      return &Result{
          Columns: []string{"version"},
          Rows:    [][]interface{}{{"FocusDB 1.0 (PostgreSQL 16.1 compatible)"}},
          Tag:     constants.ResultSelectTag(1),
      }, nil
  ```

### 3. Server Handler (`cmd/focusd/main.go`)
- **Actualización de `executeHandler.Handle()`**: Modificado para procesar múltiples statements en un solo query.
  - El handler ahora itera sobre todos los statements en el query usando `ParseStatement()` hasta llegar a EOF.
  - Retorna el resultado del **último statement ejecutado** (comportamiento estándar de PostgreSQL).
  
  **Nueva lógica:**
  ```go
  p := parser.NewParser(query)
  var lastResult *server.QueryResult

  for !p.AtEOF() {
      stmt, err := p.ParseStatement()
      if err != nil {
          return nil, err
      }
      if stmt == nil {
          continue  // Skip nil statements (semicolons)
      }

      result, err := h.executor.Execute(context.Background(), stmt)
      if err != nil {
          return nil, err
      }
      lastResult = &server.QueryResult{
          Columns: result.Columns,
          Rows:    result.Rows,
          Tag:     result.Tag,
      }
  }

  return lastResult, nil
  ```

## Tests Implementados

### 1. Unit Test (`internal/parser/parser_test.go`)
- **`TestMultipleStatements`**: Verifica que el parser pueda manejar `SET DateStyle=ISO; SELECT version();`
- **`TestSimpleSelect`**: Verifica SELECT básico.
- **`TestCreateTable`**: Verifica CREATE TABLE.

### 2. Integration Tests
- **`cmd/test-multi-stmt`**: Test simple de multi-statement parsing.
- **`cmd/test-multi-client`**: Test de conexión y ejecución multi-statement con cliente PostgreSQL.
- **`cmd/test-multi-advanced`**: Test completo con CREATE, INSERT, UPDATE, SELECT combinados.

## Resultados de Pruebas

### Test 1: Multiple SETs + SELECT ✅
```sql
SET DateStyle=ISO; SET TimeZone=UTC; SELECT version();
```
**Resultado:** Retorna versión de FocusDB.

### Test 2: CREATE + INSERT ✅
```sql
CREATE TABLE test_multi (id INT, name TEXT);
INSERT INTO test_multi (id, name) VALUES (1, 'test');
```
**Resultado:** Tabla creada y registro insertado.

### Test 3: INSERT + SELECT ✅
```sql
INSERT INTO test_multi (id, name) VALUES (2, 'another');
SELECT * FROM test_multi;
```
**Resultado:** Retorna 2 filas.

### Test 4: UPDATE + SELECT ✅
```sql
UPDATE test_multi SET name='updated' WHERE id=1;
SELECT * FROM test_multi WHERE id=1;
```
**Resultado:** Retorna fila actualizada.

## Compatibilidad

Esta implementación sigue el comportamiento estándar de PostgreSQL:
- Ejecuta todos los statements secuencialmente.
- Retorna el resultado del **último statement**.
- Si un statement falla, retorna el error y aborta la ejecución.

## Estado Final

✅ **Parser:** Soporta multi-statement parsing  
✅ **Executor:** Ejecuta statements secuencialmente  
✅ **Server:** Maneja solicitudes multi-statement  
✅ **Tests:** 7 tests pasando (3 unit + 4 integration)  
✅ **Build:** Compila sin errores  

---

## Actualización Marzo 2026 - Estabilidad de protocolo

Durante pruebas de regresión se detectó un cuelgue en cliente de protocolo extendido al ejecutar `SELECT 1`.

### Causa raíz

- El flujo de prueba en cliente esperaba `ReadyForQuery (Z)` antes de enviar `Sync`.
- En protocolo extendido, `Z` se recibe después de `Sync`.
- Adicionalmente, para consultas de sistema se enviaba `ReadyForQuery` duplicado desde el servidor, causando posible desincronización del stream de mensajes.

### Correcciones aplicadas

1. **Cliente de prueba (`cmd/test-client/main.go`)**
    - Ajuste del read loop para terminar en `CommandComplete`/`ErrorResponse` tras `Execute`.
    - Envío de `Sync` antes de esperar `ReadyForQuery`.

2. **Servidor (`internal/server/writers.go`)**
    - Eliminado `writeReady()` duplicado en `writeSystemResult()`.
    - Se mantiene un único `ReadyForQuery` por ciclo de query simple.

3. **Prueba de information schema (`cmd/test-information-schema/main.go`)**
    - Limpieza de manejo de `rows` para evitar reutilización conflictiva de resultsets.
    - Validación más robusta del contenido esperado.

### Resultado de regresión

Escenarios cliente que inicialmente fallaban y luego pasaron (exit code `0`):

- `test-client`
- `test-information-schema`
- `test-multi-advanced`
- `test-multi-client`
- `test-persistence`
- `test-simple-query`
- `test-users`

## Próximos Pasos (Opcional)

1. Añadir soporte para `IF NOT EXISTS` / `IF EXISTS` en CREATE/DROP.
2. Implementar transacciones explícitas (BEGIN/COMMIT/ROLLBACK).
3. Mejorar manejo de errores para indicar en qué statement falló.
