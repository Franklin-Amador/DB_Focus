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
- `CREATE DATABASE` nombre [WITH opciones]
- `CREATE PROCEDURE` nombre [(parámetros)] AS BEGIN sentencias... END
- `CREATE TRIGGER` nombre [BEFORE|AFTER|INSTEAD OF] [INSERT|UPDATE|DELETE] ON tabla [FOR EACH ROW] BEGIN sentencias... END
- `CREATE JOB` nombre SCHEDULE EVERY n [MINUTE|HOUR|DAY] BEGIN sentencias... END
- `CALL` procedimiento [(argumentos)]
- `DROP TRIGGER` nombre ON tabla
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
- Los triggers no se ejecutan recursivamente para evitar loops infinitos.
- Los jobs (trabajos) se ejecutan automáticamente en intervalos programados (cada N minutos/horas/días).
- `ALTER JOB` permite habilitar/deshabilitar jobs sin eliminarlos. Los cambios persisten entre reinicios.
- `ALTER TABLE` permite modificar la estructura de tablas existentes: agregar/eliminar columnas, cambiar tipos, renombrar columnas.
- **Validación de constraints en ALTER TABLE**:
  - No permite agregar una segunda PRIMARY KEY si la tabla ya tiene una
  - No permite eliminar columnas referenciadas por FOREIGN KEY de otras tablas
  - No permite eliminar columnas que son PRIMARY KEY
- Todos los cambios de schema (ALTER TABLE, ALTER JOB) persisten automáticamente en disco.
- `ORDER BY` permite ordenar resultados por una o más columnas (ASC ascendente, DESC descendente).
- `LIMIT` restringe el número de filas devueltas, `OFFSET` omite las primeras N filas (útil para paginación).

## Ejemplos de uso

```sql
-- Crear base de datos
CREATE DATABASE testing;

-- Crear tablas
CREATE TABLE users (id INT PRIMARY KEY, name TEXT, email TEXT);
CREATE TABLE orders (id INT PRIMARY KEY, user_id INT, product TEXT);

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

