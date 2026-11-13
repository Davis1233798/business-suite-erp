package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestLegacyERPImportScopesOrganizations(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "legacy.sqlite")
	target := filepath.Join(directory, "standalone.sqlite")
	db, err := openDB(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
        DROP TABLE erp_repository_schema;
        INSERT INTO organizations(id,name,slug) VALUES ('selected','Selected','selected'), ('other','Other','other');
        INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES ('warehouse-a','selected','First','A'), ('warehouse-b','other','Second','B');
        INSERT INTO users(id,email,display_name) VALUES ('legacy-user','legacy@example.test','Legacy');
    `); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := importLegacyERP(source, target, "selected"); err != nil {
		t.Fatal(err)
	}
	result, err := openDB(target)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	assertCount := func(query string, expected int) {
		t.Helper()
		var count int
		if err := result.QueryRow(query).Scan(&count); err != nil || count != expected {
			t.Fatalf("%s: got %d, want %d, error %v", query, count, expected, err)
		}
	}
	assertCount("SELECT count(*) FROM erp_warehouses WHERE id='warehouse-a' AND organization_id='selected'", 1)
	assertCount("SELECT count(*) FROM erp_warehouses", 1)
	assertCount("SELECT count(*) FROM organizations", 1)
	assertCount("SELECT count(*) FROM users", 0)
	if err := importLegacyERP(source, target, "other"); err == nil {
		t.Fatal("accepted existing destination")
	}
	legacy, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	var count int
	if err := legacy.QueryRow("SELECT count(*) FROM erp_warehouses").Scan(&count); err != nil || count != 2 {
		t.Fatalf("legacy warehouse count: %d %v", count, err)
	}
}
