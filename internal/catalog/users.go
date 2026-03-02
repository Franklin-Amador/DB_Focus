package catalog

import "fmt"

const focusUsersTable = "focus.users"

func (c *Catalog) UserExists(username string) bool {
	if userTable, err := c.GetTable(focusUsersTable); err == nil {
		if rows, err := userTable.SelectWhere("username", username); err == nil && len(rows) > 0 {
			return true
		}
	}
	return false
}

func (c *Catalog) RegisterUser(username string, superuser bool) error {
	if c.UserExists(username) {
		return nil
	}

	userTable, err := c.GetTable(focusUsersTable)
	if err != nil {
		return err
	}

	return userTable.InsertRowUnsafe([]interface{}{username, superuser, getTimestamp()})
}

func (c *Catalog) GetUser(username string) (map[string]interface{}, error) {
	userTable, err := c.GetTable(focusUsersTable)
	if err != nil {
		return nil, err
	}

	rows, err := userTable.SelectWhere("username", username)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("user %s not found", username)
	}

	row := rows[0]
	return map[string]interface{}{
		"username":   row[0],
		"superuser":  row[1],
		"created_at": row[2],
	}, nil
}

func getTimestamp() string {
	// Simple ISO 8601 timestamp - in production use time.Now()
	return "2026-02-22T16:00:00Z"
}
