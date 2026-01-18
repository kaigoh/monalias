package db

import (
	"embed"
)

//go:embed schema.sql
var schemaFS embed.FS

func Schema() (string, error) {
	data, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
