# Focus

Motor de base de datos en Go con compatibilidad PostgreSQL Wire Protocol.

## GUI — FocusDB Studio

Interfaz web incluida en el binario (`go:embed`), disponible en `http://localhost:9011` (flag `-gui`). **Funciona 100% offline**: CodeMirror 5.65.16 está vendoreado en `cmd/focusd/static/vendor/` (licencia MIT incluida).

**Bases de datos y esquemas** (jerarquía servidor → bases → esquemas → tablas)
- Dos selectores en el header: **base de datos activa** y **esquema activo**. Los statements corren dentro de la base; lo que no lleva prefijo de esquema (tablas en el editor, explorador, diagrama, autocompletado) se resuelve en el esquema activo, como la base de conexión + `search_path` de una sesión. Ambos persisten en el navegador.
- Sección **Databases** en el árbol: click activa una base (el esquema vuelve a `public`); `+` crea una (modal, `CREATE DATABASE`); `×` la elimina con todo su contenido (modal con casilla de confirmación). `postgres` es la base por defecto y no se puede eliminar.
- Sección **Schemas** (de la base activa): click activa un esquema; `+` crea uno (`CREATE SCHEMA`); `×` lo elimina (modal con opción `CASCADE` cuando no está vacío). `public` no se puede eliminar.
- Cada base es un **contenedor aislado**: tiene sus propios esquemas, procedimientos, triggers, jobs y scheduler. Una consulta no puede cruzar bases; sí puede cruzar esquemas de la misma base calificando el nombre: `SELECT p.nombre, v.monto FROM tienda.productos p JOIN public.ventas v ON p.id = v.id`.
- Por wire, el parámetro `database` de la conexión (`psql -d ventas`, `\c ventas`) elige la base; `\l` y `pg_database` listan las bases del clúster. `SET search_path TO esquema` cambia el esquema de la sesión (equivale al selector de esquema de la GUI); `SHOW search_path`, `current_schema()` y `current_database()` lo reflejan. `DROP DATABASE` no se puede ejecutar desde la base que se elimina.

**Editor**
- Pestañas múltiples de consulta (undo propio por pestaña; persisten en el navegador).
- Ejecutar statement actual (`F5`/`Ctrl+Enter`), selección, o todo el script (`Ctrl+Shift+Enter`).
- "Ejecutar todo" muestra **un resultado por statement** (acordeón); ante un error se detiene, indica cuál falló y lo subraya en el editor.
- Botón **Cancelar** para consultas largas (cancelación real vía contexto en el backend).
- Autocompletado con tablas/columnas del schema, alias del statement y snippets (`Ctrl+Space`); búsqueda en el editor (`Ctrl+F`); validación de sintaxis en vivo; historial persistente.

**Resultados**
- Orden por columna, filtro rápido, render paginado (500 filas por bloque, "cargar más").
- Export **CSV** (RFC 4180) y **JSON** del conjunto filtrado.
- Click en celda copia; doble click abre la celda expandida (pretty-print de JSON).
- Copiar fila como `INSERT`. Indicador de resultado truncado (la GUI pide `maxRows: 5000`).

**Explorador de datos** (grilla CRUD)

| Acción | Cómo |
|---|---|
| **Abrir** | Pasá el mouse sobre una tabla en el árbol lateral → aparecen dos iconos a la derecha; el de **cuadrícula** abre la grilla (el de flecha hace `SELECT *`) |
| **Paginar** | `‹` `›` en la barra superior (100 filas por página); `↺` recarga; `←` vuelve al editor |
| **Editar celda** | **Doble click** → `Enter` guarda, `Esc` cancela. Las columnas `IDENTITY` no son editables |
| **Insertar fila** | Botón `+ Insertar fila` → formulario; los campos `IDENTITY` salen deshabilitados y el `*` marca `NOT NULL` (campo vacío en columna opcional → `NULL`) |
| **Borrar fila** | Hover sobre la fila → `×` al inicio → confirmación mostrando la fila |

- Los encabezados muestran badges **PK** / **ID** (identity) / **NN** (not null).
- **Requiere `PRIMARY KEY`**: sin PK el motor no puede identificar la fila, así que la grilla queda en solo lectura (se avisa en la barra de estado y se ocultan insertar/borrar).
- Todas las escrituras se ejecutan como SQL real contra el motor: **disparan triggers** y persisten en Pebble igual que si las escribieras a mano.

**Diagrama ER**
- Posiciones, zoom y modo compacto **persisten** (localStorage, por firma del schema); reentrar a la vista no re-organiza.
- Drag fluido (solo se recalculan las líneas incidentes), pan/zoom con rueda o **pinch** (Pointer Events), minimapa con navegación.
- Self-FK como bucle, cardinalidad `1`/`N` en los extremos, contador de filas por tabla, badges de índice (`ix`) y `UNIQUE` (`u`).
- Export SVG/PNG limpio (sin labels de hover). Modo compacto real (cajas más angostas, sin tipos).

**API HTTP** (puerto GUI): `POST /api/query` (`{sql, maxRows?, database?, schema?}`), `POST /api/script` (resultados por statement + `failedIndex`; también acepta `database`/`schema`), `POST /api/validate`, `GET /api/databases` (bases con conteo de esquemas/tablas/vistas), `GET /api/schemas?database=` (esquemas de usuario con conteo de tablas/vistas), `GET /api/schema?database=&schema=` (columnas con `notNull/identity/isPK/isFK/isUnique`, `rowCount`, `indexes`, `viewDefinition`), `GET /api/objects?database=` (con `bodyText`, `lastRun`), `GET /api/diagram?database=&schema=`, `GET /api/table-data?database=&schema=&table=&offset=&limit=`. `database` vacío significa `postgres` y `schema` vacío `public`; una base o esquema inexistente responde `404`.

**Flags**: `-gui :9011` · `-query-timeout 60s` (tope por consulta de la GUI; `0` lo desactiva).

## Almacenamiento

**Backend:** Pebble (Embedded Key-Value Store con WAL)
- Persistencia ACID con Write-Ahead Logging (WAL)
- Sincronización automática en cada escritura
- Datos persisten entre reinicios del servidor
- Base de datos almacenada en: `data/pebble.db/`

## Ejecutar

```bash
go run ./cmd/focusd
```

El servidor inicia en el puerto **4444**.

## Conectar

### Con psql:
```bash
psql -h localhost -p 4444 -U postgres -d postgres
```
**Password:** `4444`

### Con pgAdmin:
- **Host:** `localhost`
- **Port:** `4444`
- **Database:** `postgres`
- **Username:** `postgres`
- **Password:** `4444`

### String de conexión:
```
postgresql://postgres:4444@localhost:4444/postgres
```

## SQL soportado

- `SELECT` [DISTINCT] columnas FROM tabla [WHERE predicado] [GROUP BY columna] [HAVING predicado] [QUALIFY predicado] [ORDER BY columna [ASC|DESC]] [LIMIT n] [OFFSET n]
  - `predicado`: `columna OP literal` combinable con `AND`/`OR` y paréntesis. `OP` puede ser `=`, `<>` (o `!=`), `<`, `>`, `<=`, `>=`.
  - Orden lógico de evaluación: `FROM/JOIN → WHERE → GROUP BY + agregados → HAVING → funciones de ventana → QUALIFY → ORDER BY → proyección → DISTINCT → LIMIT/OFFSET`.
- `SELECT` [DISTINCT] ... FROM tabla [NATURAL] [INNER|LEFT|RIGHT|FULL [OUTER]|CROSS] JOIN tabla2 [ON tabla.col = tabla2.col | USING (col, ...)] [JOIN tabla3 ...] ... [WHERE ...] [ORDER BY columna [ASC|DESC]] [LIMIT n]
  - Soporta **cadenas de N joins** (3+ tablas): `FROM a JOIN b ON ... JOIN c ON ...`. Las columnas se referencian calificadas (`tabla.col`) o sin calificar si son inequívocas.
  - `NATURAL JOIN`: une por **todas** las columnas con el mismo nombre en ambos lados; las comunes aparecen una sola vez (coalesce). Sin columnas comunes se comporta como `CROSS JOIN`.
  - `JOIN ... USING (col, ...)`: une por las columnas nombradas (que deben existir en ambos lados); esas columnas aparecen una sola vez (coalesce).
- `SELECT` agg(...) FROM tabla [GROUP BY columna] [HAVING predicado] [ORDER BY columna | agg(...) [ASC|DESC]] [LIMIT n]
  - Funciones de agregado: `COUNT(*)`, `SUM(col)`, `AVG(col)`, `MIN(col)`, `MAX(col)`.
  - `HAVING` filtra los grupos: `HAVING SUM(monto) > 100 AND n >= 2` (agregados, alias del `SELECT` o claves de `GROUP BY`; sin `GROUP BY`, toda la tabla es un grupo). Las funciones de ventana no se permiten en `HAVING` (usar `QUALIFY`). `ORDER BY` y `QUALIFY` pueden referenciar un agregado por su texto (`ORDER BY SUM(monto) DESC`, `QUALIFY SUM(monto) > 100`) aunque no esté proyectado.
- `SELECT` ... fn() OVER ([PARTITION BY col, ...] [ORDER BY col [ASC|DESC], ...]) [AS alias] ... FROM tabla [...] [QUALIFY predicado]
  - **Funciones de ventana**: `ROW_NUMBER()`, `RANK()`, `DENSE_RANK()` y los agregados `COUNT(*|col)`, `SUM`, `AVG`, `MIN`, `MAX` seguidos de `OVER (...)`. `OVER ()` vacío es válido (toda la tabla es una partición).
  - `QUALIFY`: filtra **después** de calcular las ventanas. El predicado tiene la misma forma que `WHERE` y el lado izquierdo puede ser una columna, un alias del `SELECT`, un agregado o una ventana inline: `QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY monto DESC) = 1`.
  - Funciona sobre tablas, JOINs (columnas calificadas), CTEs, vistas y consultas con `GROUP BY` — en ese caso la ventana opera sobre las filas ya agrupadas y puede referenciar agregados: `RANK() OVER (ORDER BY SUM(monto) DESC)`.
- `WITH` cte_name AS (SELECT ...) [, cte_name2 AS (SELECT ...)] SELECT ...
- `CREATE TABLE` tabla (columna tipo [IDENTITY] [PRIMARY KEY], ...)
- `CREATE VIEW` vista AS SELECT ...
- `CREATE VIEW IF NOT EXISTS` vista AS SELECT ...
- `CREATE VIEW` vista (columna1, columna2, ...) AS SELECT ...
- `CREATE OR REPLACE VIEW` vista AS SELECT ...
- `CREATE OR REPLACE VIEW` vista (columna1, columna2, ...) AS SELECT ...
- `CREATE INDEX` indice ON tabla (columna)
- `CREATE INDEX` indice ON tabla (columna1 [, columna2, ...])
- `DROP INDEX` indice ON tabla
- `CREATE SCHEMA` [IF NOT EXISTS] esquema — dentro de la base de datos actual
- `CREATE DATABASE` nombre [WITH opciones] — nueva base aislada (su propio `public`, procedimientos, triggers y jobs)
- `DROP DATABASE` nombre — elimina la base con todo su contenido (no la por defecto ni la que está en uso)
- `CREATE PROCEDURE` nombre [(parámetros)] AS BEGIN sentencias... END
- `CREATE TRIGGER` nombre [BEFORE|AFTER|INSTEAD OF] [INSERT|UPDATE|DELETE] ON tabla [FOR EACH ROW] BEGIN sentencias... END
- `CREATE JOB` nombre SCHEDULE EVERY n [MINUTE|HOUR|DAY] BEGIN sentencias... END
- `CALL` procedimiento [(argumentos)]
- `DROP TRIGGER` nombre ON tabla
- `DROP TABLE` [IF EXISTS] tabla [CASCADE | RESTRICT]
- `DROP SCHEMA` [IF EXISTS] schema [CASCADE | RESTRICT]
- `DROP VIEW` vista [CASCADE | RESTRICT]
- `DROP VIEW IF EXISTS` vista [CASCADE | RESTRICT]
- `DROP JOB` nombre
- `ALTER JOB` nombre [ENABLE|DISABLE]
- `ALTER TABLE` tabla ADD COLUMN columna tipo [IDENTITY] [PRIMARY KEY]
- `ALTER TABLE` tabla DROP COLUMN columna
- `ALTER TABLE` tabla ALTER COLUMN columna TYPE nuevo_tipo
- `ALTER TABLE` tabla RENAME COLUMN nombre_viejo TO nombre_nuevo
- `INSERT INTO` tabla [(columna, ...)] VALUES (literal, ...)
- `UPDATE` tabla SET columna = valor [WHERE columna = literal]
- `DELETE FROM` tabla [WHERE columna = literal]

**Notas:**
- `WHERE` soporta operadores de comparación: `=`, `<>`/`!=`, `<`, `>`, `<=`, `>=` (en SELECT, UPDATE y DELETE). La igualdad (`=`) usa índice cuando existe. **Todos** los operadores comparan por forma canónica: numéricamente si ambos lados son números, lexicográficamente en caso contrario. Esto hace que `WHERE id = 1` funcione igual sobre una columna `IDENTITY` (valor `int`) que sobre una columna con valores literales (`string`) — importante en `UPDATE`/`DELETE`, que recorren la tabla en vez de usar el índice.
- `WHERE` soporta predicados compuestos con `AND` y `OR`, precedencia estándar (`AND` liga más fuerte que `OR`) y paréntesis para agrupar: `WHERE a > 10 AND (b = 'x' OR c <= 5)`. Funciona también sobre JOINs (columnas calificadas, ej. `pedidos.total > 80`).
- **Funciones de ventana**: dentro de cada partición (`PARTITION BY`, o toda la tabla si se omite) las filas se ordenan por el `ORDER BY` de la ventana; las filas iguales en todas las claves son *pares*. `ROW_NUMBER` numera 1..n; `RANK` deja huecos tras un empate (1,1,3); `DENSE_RANK` no (1,1,2). Los agregados `OVER` sin `ORDER BY` cubren toda la partición; con `ORDER BY` usan el frame por defecto del estándar (`RANGE UNBOUNDED PRECEDING → CURRENT ROW`): acumulado hasta la fila actual incluyendo sus pares. No hay frames explícitos (`ROWS BETWEEN ...`) ni `LAG/LEAD`.
- `QUALIFY` se evalúa después de las ventanas y antes de `ORDER BY`/`LIMIT`, así que `QUALIFY rn <= 3 ORDER BY monto DESC LIMIT 5` filtra primero y recorta después. Si la consulta no tiene ventanas, `QUALIFY` actúa como un filtro sobre alias/columnas de salida (extensión permisiva). Una ventana usada solo en `QUALIFY` no aparece en el resultado (tampoco con `SELECT *`).
- `ORDER BY` acepta alias del `SELECT` y agregados (`ORDER BY total DESC`, `ORDER BY SUM(monto) DESC`) y ahora falla con `ORDER BY: column X not found` si la columna no existe (antes se ignoraba en silencio). Un agregado en `ORDER BY`/`QUALIFY`/`OVER` exige `GROUP BY` o un agregado en el `SELECT`; en consultas agrupadas, una columna referenciada fuera del `SELECT` debe ser clave de `GROUP BY` o ir dentro de un agregado (`column X must appear in GROUP BY ...`).
- `QUALIFY`, `OVER` y `PARTITION` son palabras reservadas desde esta versión (igual que `ORDER`, `LIMIT`, etc.). Una columna o tabla con ese nombre debe escribirse entre comillas dobles: `SELECT "partition" FROM logs`.
- Un agregado con argumento que no sea `*`, una columna o un agregado anidado (`COUNT(DISTINCT x)`, `SUM(a * 2)`) es un error de sintaxis explícito; antes parseaba y devolvía `NULL`. Una ventana envuelta en una expresión (`ROW_NUMBER() OVER () * 2`) se proyecta como `NULL`, como cualquier otra expresión no soportada.
- Columnas con `IDENTITY` se auto-incrementan automáticamente en cada INSERT.
- `INSERT` sin lista de columnas ahora soporta correctamente tablas con `IDENTITY`: si envías solo los valores de columnas no-IDENTITY, el motor autogenera el ID y preserva el resto de valores en su columna correcta.
- Los procedimientos almacenados pueden tener parámetros y ejecutar múltiples sentencias.
- Los triggers se ejecutan automáticamente en respuesta a eventos INSERT, UPDATE o DELETE.
- Los triggers pueden ejecutarse recursivamente, con límite de profundidad para evitar loops infinitos (profundidad máxima actual: 16).
- Los jobs (trabajos) se ejecutan automáticamente en intervalos programados (cada N minutos/horas/días).
- Vistas con lista explícita de columnas: permite renombrar columnas en la definición de vista (ej: `CREATE VIEW v (col1, col2) AS SELECT a, b FROM t`).
- `CREATE VIEW IF NOT EXISTS`: permite creación idempotente (no error si la vista ya existe).
- Validación de vistas: el número de columnas en la lista debe coincidir con las columnas retornadas por la consulta SELECT.
- La lista explícita de columnas en vistas es case-insensitive y rechaza duplicados.
- `DROP VIEW CASCADE`: Elimina la vista y todas las vistas que dependen de ella (cascada de eliminación).
- `DROP VIEW RESTRICT`: Elimina la vista solo si no hay vistas dependientes; de lo contrario, falla. Este es el comportamiento por defecto.
- `DROP VIEW IF EXISTS [CASCADE | RESTRICT]`: Combina seguridad (no error si no existe) con manejo de dependencias.
- Las vistas pueden formar cadenas de dependencias: vista A -> vista B -> vista C. CASCADE elimina la cadena completa.
- `DROP TABLE RESTRICT` (default): bloquea el borrado si hay FKs o vistas dependientes.
- `DROP TABLE CASCADE`: elimina la tabla y limpia dependencias (FKs en tablas referenciantes y vistas que dependen de la tabla).
- `DROP TABLE IF EXISTS`: no retorna error si la tabla no existe.
- `DROP SCHEMA RESTRICT` (default): bloquea el borrado si el schema contiene tablas o vistas.
- `DROP SCHEMA CASCADE`: elimina el schema junto con sus tablas y vistas.
- `DROP SCHEMA IF EXISTS`: no retorna error si el schema no existe.
- `public`, `pg_catalog`, `information_schema` y `pg_toast` no se pueden eliminar ni con `CASCADE`.
- Referencias a tablas: `FROM esquema.tabla [AS] alias`, también con alias sin `AS` (`FROM ventas v`). Las columnas pueden calificarse con el alias, el nombre de la tabla o `esquema.tabla` (`WHERE v.monto > 1`, `SELECT tienda.productos.nombre ...`). Sin calificar, una tabla se busca en el esquema de la sesión (parámetro `database` del wire protocol, o esquema activo de la GUI) y por defecto en `public`.
- Una vista resuelve sus tablas sin calificar dentro de **su propio esquema**, también tras reiniciar el servidor (`CREATE VIEW caros AS SELECT ... FROM productos` creada con el esquema `tienda` activo lee `tienda.productos`).
- `pg_catalog.pg_namespace` y `\dn` listan los esquemas de usuario de la base actual; `pg_database` y `\l`, las bases del clúster.
- **Bases de datos**: cada una es un catálogo independiente. Los datos se guardan en Pebble con claves `db:<base>:...`. Un directorio de datos creado antes de esta versión se **migra automáticamente** al arrancar, una sola vez: antes de tocar nada se copia el almacén a `pebble.db.backup-<fecha>` (junto al original; se puede borrar cuando todo esté verificado), los esquemas creados con el antiguo `CREATE DATABASE` pasan a ser bases reales (sus tablas quedan en su `public`, así `psql -d nombre` sigue funcionando), el resto de esquemas queda dentro de `postgres`, y procedimientos, triggers y jobs (antes globales) pertenecen a `postgres`.
- Los nombres de base, esquema, tabla y vista no pueden contener `:` (separador de las claves de almacenamiento).
- `ALTER JOB` permite habilitar/deshabilitar jobs sin eliminarlos. Los cambios persisten entre reinicios.
- `ALTER TABLE` permite modificar la estructura de tablas existentes: agregar/eliminar columnas, cambiar tipos, renombrar columnas.
- `CREATE INDEX` permite acelerar búsquedas por igualdad (`WHERE columna = valor`) en tablas con alto volumen de filas.
- `DROP INDEX` elimina un índice definido en una tabla y persiste el cambio en disco.
- Soporta claves foráneas autorreferenciadas (self FK), por ejemplo `FOREIGN KEY (parent_id) REFERENCES categorias(id)`.
- Las claves foráneas funcionan correctamente cuando referencian una columna `IDENTITY`: la comparación de valores es tolerante al tipo, así que un valor literal (`1`) coincide con una clave IDENTITY autogenerada.
- Los JOIN encadenan N tablas (3+). La condición `ON` compara por forma canónica, por lo que un JOIN entre una FK literal y una PK `IDENTITY` empareja correctamente.
- `NATURAL JOIN` y `USING` coalescen las columnas comunes (aparecen una vez). En outer joins el valor coalescido toma el lado no nulo. La columna común sobrevive con la referencia de la tabla izquierda (ej. tras `a NATURAL JOIN b`, se usa `a.col` o `col` sin calificar; `b.col` deja de estar disponible).
- **Validación de constraints en ALTER TABLE**:
  - No permite agregar una segunda PRIMARY KEY si la tabla ya tiene una
  - No permite eliminar columnas referenciadas por FOREIGN KEY de otras tablas
  - No permite eliminar columnas que son PRIMARY KEY
- Todos los cambios de schema (ALTER TABLE, ALTER JOB) persisten automáticamente en disco.
- Las definiciones de índices también persisten en disco y se reconstruyen al reiniciar.
- `ORDER BY` permite ordenar resultados por una o más columnas (ASC ascendente, DESC descendente).
- `LIMIT` restringe el número de filas devueltas, `OFFSET` omite las primeras N filas (útil para paginación).
- En `psql`, `\dt` lista solo tablas y `\dv` lista solo vistas.

## Indices

Guia extendida: ver `INDEXING.md`.

Sintaxis soportada:

```sql
CREATE INDEX nombre_indice ON tabla (columna);
CREATE INDEX nombre_indice_compuesto ON tabla (columna1, columna2);
```

Comportamiento actual:

- Optimizados para filtros de igualdad: `WHERE columna = valor`.
- Soporta indices simples y compuestos (multi-columna).
- Se mantienen consistentes en `INSERT`, `UPDATE` y `DELETE`.
- Persisten en disco y se rehidratan al reiniciar el motor.
- Se actualizan automáticamente al renombrar o eliminar columnas con `ALTER TABLE`.

Resumen rapido de optimizacion:

| Patron de consulta | Estado |
| --- | --- |
| `WHERE columna = valor` | Si optimiza |
| `WHERE col1 = valor` con indice compuesto `(col1, col2)` | Si optimiza (prefijo de la primera columna) |
| `WHERE columna > valor` | No optimiza (scan) |
| `WHERE columna < valor` | No optimiza (scan) |
| `WHERE columna BETWEEN a AND b` | No optimiza (scan) |
| Indice compuesto `(col1, col2)` | Soportado |

Limitaciones actuales:

- La optimizacion de indices compuestos actualmente aplica a busquedas por igualdad sobre la primera columna del indice.
- Sin optimizacion de rangos (`>`, `<`, `BETWEEN`).
- `DROP INDEX` ya está soportado para eliminación explícita de índices por tabla.

## Pruebas de regresión recomendadas

> **Entorno Windows**: compilar/ejecutar con `CGO_ENABLED=0` si el toolchain C no está en el PATH
> (Pebble tiene implementación puro-Go). Ej: `CGO_ENABLED=0 go run ./cmd/test-multi-join`.

```bash
# Escenarios de integración del motor (sin servidor externo)

# DDL / ALTER / schemas
go run ./cmd/test-alter
go run ./cmd/test-alter-constraints
go run ./cmd/test-create-schema
go run ./cmd/test-drop-schema
go run ./cmd/test-schemas          # esquema activo, alias, vistas por esquema, DROP DATABASE, recarga
go run ./cmd/test-parse-ddl

# API HTTP de la GUI (httptest, sin puerto)
go test ./cmd/focusd

# Índices
go run ./cmd/test-index
go run ./cmd/test-drop-index

# Claves foráneas e IDENTITY
go run ./cmd/test-drop-table-fk
go run ./cmd/test-self-fk
go run ./cmd/test-fk-identity
go run ./cmd/test-identity-insert

# Consultas: WHERE, agregados, JOINs, ventanas + QUALIFY
go run ./cmd/test-where-operators
go run ./cmd/test-aggregates
go run ./cmd/test-multi-join
go run ./cmd/test-natural-join
go run ./cmd/test-qualify

# Vistas
go run ./cmd/test-views
go run ./cmd/test-views-cascade
go run ./cmd/test-views-columnlist
go run ./cmd/test-view-if-not-exists
go run ./cmd/test-view-persistence

# Rutinas (procedures / triggers / jobs) y persistencia
go run ./cmd/test-parse-proc
go run ./cmd/test-procedure-persistence
go run ./cmd/test-routine-persistence
go run ./cmd/test-job-persistence
go run ./cmd/test-trigger-recursion
go run ./cmd/test-persistence-integration

# Parseo multi-statement
go run ./cmd/test-multi-stmt
go run ./cmd/test-multiline
```

```bash
# Escenarios cliente/wire protocol (requieren servidor)
# Terminal 1
go run ./cmd/focusd -addr :4444 -data ./data_regression_4444

# Terminal 2
go run ./cmd/focusd -addr :4445 -data ./data_regression_4445

# Terminal 3
go run ./cmd/test-client
go run ./cmd/test-information-schema
go run ./cmd/test-multi-advanced
go run ./cmd/test-multi-client
go run ./cmd/test-persistence
go run ./cmd/test-simple-query
go run ./cmd/test-users
```

## Notas de protocolo (Marzo 2026)

- Se corrigió un cuelgue en flujo extendido (`Parse/Bind/Describe/Execute/Sync`) al ejecutar `SELECT 1` en cliente de prueba.
- Se eliminó una emisión duplicada de `ReadyForQuery` en respuestas de consultas de sistema para evitar desincronización del stream.

## Ejemplos de uso

```sql
-- Crear base de datos
CREATE DATABASE testing;

-- Crear tablas
CREATE TABLE users (id INT PRIMARY KEY, name TEXT, email TEXT);
CREATE TABLE orders (id INT PRIMARY KEY, user_id INT, product TEXT);

-- Crear vista
CREATE VIEW v_users AS SELECT id, name FROM users;

-- Crear vista solo si no existe
CREATE VIEW IF NOT EXISTS v_users AS SELECT id, name FROM users;

-- Crear vista con lista explícita de columnas
CREATE VIEW v_users_renamed (user_id, user_name) AS SELECT id, name FROM users;

-- Reemplazar vista
CREATE OR REPLACE VIEW v_users AS SELECT id FROM users;

-- Reemplazar vista con nombres de columnas explícitos
CREATE OR REPLACE VIEW v_users_explicit (uid, uname, uemail) AS SELECT id, name, email FROM users;

-- Insertar datos
INSERT INTO users VALUES (1, 'Estiven', 'thegoat@gmail.com');
INSERT INTO users VALUES (2, 'Bob', 'bob@test.com');
INSERT INTO orders VALUES (1, 1, 'laptop');
INSERT INTO orders VALUES (2, 1, 'mouse');
INSERT INTO orders VALUES (3, 2, 'keyboard');

-- DISTINCT: eliminar duplicados
SELECT DISTINCT user_id FROM orders;

-- DISTINCT con múltiples columnas
SELECT DISTINCT * FROM orders;

-- DISTINCT con JOIN
SELECT DISTINCT users.name FROM users INNER JOIN orders ON users.id = orders.user_id;

-- Agregar COUNT: contar todas las filas (COUNT(*))
SELECT COUNT(*) FROM users;

-- Funciones de agregado: SUM, AVG, MIN, MAX
SELECT SUM(precio) FROM productos;
SELECT AVG(precio) FROM productos;
SELECT MIN(precio) FROM productos;
SELECT MAX(precio) FROM productos;

-- GROUP BY: agrupar y contar/agregar por columna
SELECT user_id, COUNT(*) FROM orders GROUP BY user_id;
SELECT categoria, SUM(precio) FROM productos GROUP BY categoria;
SELECT categoria, MAX(precio) FROM productos GROUP BY categoria;
SELECT categoria, SUM(precio) FROM productos GROUP BY categoria ORDER BY SUM(precio) DESC;

-- Funciones de ventana: ranking por partición y agregados OVER
SELECT categoria, nombre, precio,
       ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY precio DESC) AS rn,
       SUM(precio)  OVER (PARTITION BY categoria) AS total_categoria
FROM productos;

-- QUALIFY: el producto más caro de cada categoría (top-N por grupo)
SELECT categoria, nombre, precio
FROM productos
QUALIFY ROW_NUMBER() OVER (PARTITION BY categoria ORDER BY precio DESC) = 1;

-- QUALIFY por alias + ORDER BY + LIMIT
SELECT categoria, nombre, precio, RANK() OVER (PARTITION BY categoria ORDER BY precio DESC) AS pos
FROM productos
QUALIFY pos <= 3
ORDER BY categoria, pos
LIMIT 20;

-- Ventana sobre GROUP BY: ranking de categorías por ventas totales
SELECT categoria, SUM(precio) AS total, RANK() OVER (ORDER BY SUM(precio) DESC) AS pos
FROM productos
GROUP BY categoria
QUALIFY pos <= 2;

-- INNER JOIN: solo filas con coincidencias
SELECT * FROM users INNER JOIN orders ON users.id = orders.user_id;

-- LEFT JOIN: todas las filas de users + coincidencias de orders (NULL si no hay coincidencia)
SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id;

-- RIGHT JOIN: todas las filas de orders + coincidencias de users (NULL si no hay coincidencia)
SELECT * FROM users RIGHT JOIN orders ON users.id = orders.user_id;

-- FULL OUTER JOIN: todas las filas de ambas tablas (NULL donde no coinciden)
SELECT * FROM users FULL OUTER JOIN orders ON users.id = orders.user_id;

-- CROSS JOIN: producto cartesiano (todas las combinaciones de users × orders)
SELECT * FROM users CROSS JOIN orders;

-- SELF JOIN: tabla consigo misma usando aliases (ejemplo: empleados y sus gerentes)
SELECT e1.name AS employee, e2.name AS manager 
FROM employees AS e1 INNER JOIN employees AS e2 ON e1.manager_id = e2.id;

-- SELECT con columnas específicas
SELECT users.name, orders.product FROM users INNER JOIN orders ON users.id = orders.user_id;

-- Consultar vista
SELECT * FROM v_users;

-- Eliminar vista
DROP VIEW v_users;

-- Eliminar vista sin error si no existe
DROP VIEW IF EXISTS v_users;

-- Eliminar vista con CASCADE (elimina vistas dependientes)
DROP VIEW v_users CASCADE;

-- Eliminar vista con RESTRICT (error si existen vistas dependientes - es el comportamiento por defecto)
DROP VIEW v_users RESTRICT;

-- Eliminar vista con IF EXISTS y CASCADE
DROP VIEW IF EXISTS v_users CASCADE;

-- Eliminar tabla (RESTRICT por defecto)
DROP TABLE users;

-- Eliminar tabla con IF EXISTS
DROP TABLE IF EXISTS users;

-- Eliminar tabla y limpiar dependencias
DROP TABLE users CASCADE;

-- Eliminar schema (RESTRICT por defecto)
DROP SCHEMA myschema;

-- Eliminar schema con CASCADE
DROP SCHEMA myschema CASCADE;

-- Eliminar schema con IF EXISTS
DROP SCHEMA IF EXISTS myschema CASCADE;

-- WITH (Common Table Expressions - CTEs): tablas temporales para la consulta
WITH subset AS (SELECT * FROM users WHERE id = 1)
SELECT * FROM subset;

-- WITH con JOIN: filtrar gerentes y luego hacer JOIN
WITH managers AS (SELECT * FROM employees WHERE manager_id = 0)
SELECT e.name AS employee, m.name AS manager 
FROM employees AS e INNER JOIN managers AS m ON e.manager_id = m.id;

-- WITH con múltiples CTEs
WITH subset1 AS (SELECT * FROM users WHERE id = 1),
     subset2 AS (SELECT * FROM users WHERE id = 2)
SELECT * FROM subset1;

-- IDENTITY: columnas auto-incrementales
CREATE TABLE productos (id INTEGER IDENTITY PRIMARY KEY, nombre TEXT, precio INTEGER);

-- INSERT sin especificar columna IDENTITY (se auto-incrementa)
INSERT INTO productos (nombre, precio) VALUES ('Laptop', 1000);
INSERT INTO productos (nombre, precio) VALUES ('Mouse', 25);
INSERT INTO productos (nombre, precio) VALUES ('Teclado', 50);

-- También funciona sin lista de columnas cuando solo falta la columna IDENTITY
CREATE TABLE test (id INTEGER IDENTITY PRIMARY KEY, name TEXT);
INSERT INTO test VALUES ('Estiven');
INSERT INTO test VALUES ('Oscar');

-- Ver resultados con IDs auto-generados
SELECT * FROM productos;
-- Resultado:
--  id | nombre  | precio
-- ----+---------+--------
--  1  | Laptop  | 1000
--  2  | Mouse   | 25
--  3  | Teclado | 50

-- INDICES: acelerar filtros por igualdad

-- Crear indice por edad
CREATE INDEX idx_users_age ON users (age);

-- Consulta optimizada por indice (equality lookup)
SELECT * FROM users WHERE age = 30;

-- ORDER BY: ordenar resultados por una o más columnas

-- Ordenar por precio ascendente (menor a mayor) - ASC es el default
SELECT * FROM productos ORDER BY precio;
SELECT * FROM productos ORDER BY precio ASC;

-- Ordenar por precio descendente (mayor a menor)
SELECT * FROM productos ORDER BY precio DESC;

-- Ordenar por nombre alfabéticamente
SELECT * FROM productos ORDER BY nombre;

-- Ordenar por múltiples columnas (precio DESC, luego nombre ASC)
SELECT * FROM productos ORDER BY precio DESC, nombre ASC;

-- LIMIT: limitar número de resultados

-- Obtener solo los primeros 2 productos
SELECT * FROM productos LIMIT 2;
-- Resultado:
--  id | nombre | precio
-- ----+--------+--------
--  1  | Laptop | 1000
--  2  | Mouse  | 25

-- Combinar ORDER BY y LIMIT: obtener los 2 productos más caros
SELECT * FROM productos ORDER BY precio DESC LIMIT 2;
-- Resultado:
--  id | nombre  | precio
-- ----+---------+--------
--  1  | Laptop  | 1000
--  3  | Teclado | 50

-- Combinar ORDER BY y LIMIT: obtener los 2 productos más baratos
SELECT * FROM productos ORDER BY precio ASC LIMIT 2;
-- Resultado:
--  id | nombre  | precio
-- ----+---------+--------
--  2  | Mouse   | 25
--  3  | Teclado | 50

-- OFFSET: saltar las primeras N filas (útil para paginación)

-- Página 1: primeros 2 productos (LIMIT 2 OFFSET 0)
SELECT * FROM productos ORDER BY id LIMIT 2 OFFSET 0;

-- Página 2: siguientes 2 productos (LIMIT 2 OFFSET 2)
SELECT * FROM productos ORDER BY id LIMIT 2 OFFSET 2;

-- Página 3: siguientes 2 productos (LIMIT 2 OFFSET 4)
SELECT * FROM productos ORDER BY id LIMIT 2 OFFSET 4;

-- Saltar el producto más barato y obtener los siguientes 2
SELECT * FROM productos ORDER BY precio ASC LIMIT 2 OFFSET 1;

-- PROCEDIMIENTOS ALMACENADOS: encapsular lógica reutilizable

-- Crear procedimiento sin parámetros
CREATE PROCEDURE agregar_laptop() AS BEGIN
  INSERT INTO productos (nombre, precio) VALUES ('Laptop HP', 850);
END;

-- Llamar procedimiento sin parámetros
CALL agregar_laptop();

-- Crear procedimiento con parámetros (INSERT)
CREATE PROCEDURE insertar_producto(nombre TEXT, precio INTEGER) AS BEGIN
  INSERT INTO productos (nombre, precio) VALUES (nombre, precio);
END;

-- Llamar procedimiento con argumentos
CALL insertar_producto('Webcam', 120);
CALL insertar_producto('Teclado Mecánico', 150);
CALL insertar_producto('Mouse Gamer', 80);

-- Crear procedimiento con parámetros (UPDATE)
CREATE PROCEDURE cambiar_precio(prod TEXT, nuevo_precio INTEGER) AS BEGIN
  UPDATE productos SET precio = nuevo_precio WHERE nombre = prod;
END;

-- Llamar procedimiento UPDATE
CALL cambiar_precio('Webcam', 99);

-- Crear procedimiento con parámetros (DELETE)
CREATE PROCEDURE eliminar_producto(prod TEXT) AS BEGIN
  DELETE FROM productos WHERE nombre = prod;
END;

-- Llamar procedimiento DELETE
CALL eliminar_producto('Mouse Gamer');

-- Ver resultados después de usar procedimientos
SELECT * FROM productos;
-- Resultado:
--  id | nombre           | precio
-- ----+------------------+--------
--  1  | Laptop           | 1000
--  2  | Mouse            | 25
--  3  | Teclado          | 50
--  4  | Laptop HP        | 850
--  5  | Webcam           | 99
--  6  | Teclado Mecánico | 150

-- TRIGGERS: ejecutar lógica automática en respuesta a eventos

-- Crear tabla de auditoría
CREATE TABLE auditoria (
  id INTEGER IDENTITY PRIMARY KEY,
  accion TEXT,
  mensaje TEXT,
  fecha TEXT
);

-- Crear trigger AFTER INSERT: registrar cuando se inserta un producto
CREATE TRIGGER log_insert AFTER INSERT ON productos FOR EACH ROW BEGIN
  INSERT INTO auditoria (accion, mensaje, fecha) 
  VALUES ('INSERT', 'Nuevo producto añadido', 'NOW');
END;

-- Insertar producto (dispara el trigger automáticamente)
INSERT INTO productos (nombre, precio) VALUES ('Monitor', 350);

-- Ver auditoría
SELECT * FROM auditoria;
-- Resultado:
--  id | accion | mensaje                | fecha
-- ----+--------+------------------------+-------
--  1  | INSERT | Nuevo producto añadido | NOW

-- Crear trigger BEFORE UPDATE: validar antes de actualizar
CREATE TRIGGER validar_precio BEFORE UPDATE ON productos FOR EACH ROW BEGIN
  INSERT INTO auditoria (accion, mensaje, fecha)
  VALUES ('UPDATE', 'Precio actualizado', 'NOW');
END;

-- Actualizar producto (dispara el trigger)
UPDATE productos SET precio = 300 WHERE nombre = 'Monitor';

-- Crear trigger AFTER DELETE: registrar eliminaciones
CREATE TRIGGER log_delete AFTER DELETE ON productos FOR EACH ROW BEGIN
  INSERT INTO auditoria (accion, mensaje, fecha)
  VALUES ('DELETE', 'Producto eliminado', 'NOW');
END;

-- Eliminar producto (dispara el trigger)
DELETE FROM productos WHERE nombre = 'Mouse';

-- Ver todas las acciones registradas
SELECT * FROM auditoria;
-- Resultado muestra todos los eventos: INSERT, UPDATE, DELETE

-- Eliminar trigger
DROP TRIGGER log_insert ON productos;

-- JOBS: ejecutar tareas automáticamente en intervalos programados

-- Crear tabla de respaldos
CREATE TABLE respaldos (
  id INTEGER IDENTITY PRIMARY KEY,
  total_productos INTEGER,
  fecha TEXT
);

-- Crear job que se ejecuta cada 5 minutos: cuenta productos
CREATE JOB backup_productos SCHEDULE EVERY 5 MINUTE BEGIN
  INSERT INTO respaldos (total_productos, fecha)
  VALUES ((SELECT COUNT(*) FROM productos), 'NOW');
END;

-- Crear job que se ejecuta cada hora: limpia auditoría antigua
CREATE JOB limpieza_auditoria SCHEDULE EVERY 1 HOUR BEGIN
  DELETE FROM auditoria WHERE accion = 'INSERT';
END;

-- Crear job que se ejecuta cada día: resumen diario
CREATE JOB resumen_diario SCHEDULE EVERY 1 DAY BEGIN
  INSERT INTO auditoria (accion, mensaje, fecha)
  VALUES ('RESUMEN', 'Resumen diario generado', 'NOW');
END;

-- El job se ejecutará automáticamente cada 5 minutos
-- Esperar y verificar que se ejecutó
SELECT * FROM respaldos;
-- Resultado:
--  id | total_productos | fecha
-- ----+-----------------+-------
--  1  | 7               | NOW

-- Deshabilitar job temporalmente
ALTER JOB backup_productos DISABLE;

-- Habilitar job nuevamente
ALTER JOB backup_productos ENABLE;

-- Eliminar job
DROP JOB limpieza_auditoria;

-- VALIDACIONES DE CONSTRAINTS EN ALTER TABLE

-- No se permite agregar una segunda PRIMARY KEY
ALTER TABLE users ADD COLUMN email TEXT PRIMARY KEY;
-- ERROR: table public.users already has a primary key on column id

-- No se puede eliminar columna referenciada por FOREIGN KEY
-- Si orders.user_id tiene FK a users.id:
ALTER TABLE users DROP COLUMN id;
-- ERROR: cannot drop column id: it is referenced by foreign key in table public.orders column user_id

-- No se puede eliminar columna PRIMARY KEY
ALTER TABLE users DROP COLUMN id;
-- ERROR: cannot drop column id: it is a primary key

-- Sí se puede eliminar columna sin constraints
ALTER TABLE users DROP COLUMN name;
-- SUCCESS: columna eliminada correctamente

