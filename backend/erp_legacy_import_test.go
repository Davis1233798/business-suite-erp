package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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

func TestLegacyERPImportRejectsUnbalancedJournal(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.sqlite")
	target := filepath.Join(dir, "target.sqlite")
	db, err := openDB(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
        INSERT INTO organizations(id,name,slug) VALUES ('org','Example','org');
        INSERT INTO erp_journal_entries(id,organization_id,entry_number,entry_date,description,status)
            VALUES ('journal','org','JE-1','2026-10-01','Example','posted');
        INSERT INTO erp_journal_lines(id,organization_id,journal_id,account_code,account_name,debit_amount,credit_amount)
            VALUES ('line','org','journal','1100','Cash',100,0);
    `)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := importLegacyERP(source, target, "org"); err == nil || !strings.Contains(err.Error(), "unbalanced") {
		t.Fatalf("expected reconciliation failure, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("unexpected destination: %v", err)
	}
}

func TestLegacyERPImportBackfillsShipmentCost(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.sqlite")
	target := filepath.Join(dir, "target.sqlite")
	db, err := openDB(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
        ALTER TABLE erp_shipment_lines DROP COLUMN extended_cost;
        INSERT INTO organizations(id,name,slug) VALUES ('org','Example','org');
        INSERT INTO erp_shipment_lines(id,organization_id,shipment_id,item_id,quantity,unit_cost)
            VALUES ('line','org','shipment','item',3,125);
    `)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := importLegacyERP(source, target, "org"); err != nil {
		t.Fatal(err)
	}
	result, err := openDB(target)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	var cost int
	if err := result.QueryRow("SELECT extended_cost FROM erp_shipment_lines WHERE id='line'").Scan(&cost); err != nil || cost != 375 {
		t.Fatalf("shipment cost: got %d, want 375, error %v", cost, err)
	}
}
