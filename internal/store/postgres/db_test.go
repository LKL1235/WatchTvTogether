package postgres

import (
	"strings"
	"testing"
)

func TestEmbeddedSchemaCreatesEmailColumn(t *testing.T) {
	schema := embeddedSchema(t)

	assertSchemaContains(t, schema, "email TEXT NOT NULL DEFAULT ''")
	assertSchemaContains(t, schema, "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_lower ON users (LOWER(email)) WHERE email <> ''")
}

func TestEmbeddedSchemaUpgradesLegacyUsersTable(t *testing.T) {
	schema := embeddedSchema(t)

	assertSchemaContains(t, schema, "ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''")
}

func embeddedSchema(t *testing.T) string {
	t.Helper()

	raw, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

func assertSchemaContains(t *testing.T, schema, statement string) {
	t.Helper()

	if !strings.Contains(schema, statement) {
		t.Fatalf("embedded schema does not contain %q", statement)
	}
}
