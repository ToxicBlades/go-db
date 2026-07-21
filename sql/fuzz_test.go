package sql

import "testing"

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"SELECT * FROM users",
		"SELECT id FROM users WHERE id >= 2 AND name != 'Ada';",
		"INSERT INTO users (id, name) VALUES (1, 'Ada')",
		"CREATE TABLE users (id INT, name STRING NOT NULL UNIQUE)",
		"EXPLAIN SELECT * FROM users",
		"SELECT 'unterminated",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = Parse(input)
	})
}
