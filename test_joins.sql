-- Verify test data
SELECT * FROM users;
SELECT * FROM orders;

-- Test CROSS JOIN (cartesian product)
SELECT * FROM users CROSS JOIN orders;

-- Test FULL OUTER JOIN
SELECT * FROM users FULL OUTER JOIN orders ON users.id = orders.user_id;

-- Test FULL JOIN (same as FULL OUTER)
SELECT * FROM users FULL JOIN orders ON users.id = orders.user_id;
