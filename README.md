# Focus

Motor de base de datos en Go con compatibilidad PostgreSQL Wire Protocol.

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

- `SELECT` [DISTINCT] columnas FROM tabla [WHERE columna = literal] [GROUP BY columna] [ORDER BY columna [ASC|DESC]] [LIMIT n] [OFFSET n]
- `SELECT` [DISTINCT] ... FROM tabla [INNER|LEFT|RIGHT|FULL [OUTER]|CROSS] JOIN tabla2 [ON tabla.col = tabla2.col] [ORDER BY columna [ASC|DESC]] [LIMIT n]
- `SELECT` COUNT(*) FROM tabla [GROUP BY columna] [ORDER BY columna [ASC|DESC]] [LIMIT n]
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
- `CREATE DATABASE` nombre [WITH opciones]
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
- Columnas con `IDENTITY` se auto-incrementan automáticamente en cada INSERT.
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
- `ALTER JOB` permite habilitar/deshabilitar jobs sin eliminarlos. Los cambios persisten entre reinicios.
- `ALTER TABLE` permite modificar la estructura de tablas existentes: agregar/eliminar columnas, cambiar tipos, renombrar columnas.
- `CREATE INDEX` permite acelerar búsquedas por igualdad (`WHERE columna = valor`) en tablas con alto volumen de filas.
- `DROP INDEX` elimina un índice definido en una tabla y persiste el cambio en disco.
- Soporta claves foráneas autorreferenciadas (self FK), por ejemplo `FOREIGN KEY (parent_id) REFERENCES categorias(id)`.
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

```bash
# Escenarios de integración del motor (sin servidor externo)
go run ./cmd/test-alter
go run ./cmd/test-alter-constraints
go run ./cmd/test-create-schema
go run ./cmd/test-drop-table-fk
go run ./cmd/test-index
go run ./cmd/test-job-persistence
go run ./cmd/test-multi-stmt
go run ./cmd/test-parse-proc
go run ./cmd/test-persistence-integration
go run ./cmd/test-procedure-persistence
go run ./cmd/test-self-fk
go run ./cmd/test-trigger-recursion
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

-- GROUP BY: agrupar y contar por columna
SELECT user_id, COUNT(*) FROM orders GROUP BY user_id;

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

