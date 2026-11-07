package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestERPBackupPreservesPurchaseAndRetryWithoutChangingSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "erp-live.sqlite")
	db, err := openDB(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	active := &App{db: db}
	admin := login(t, active, "admin@suite.test", token())
	org, supplier, item, _ := purchaseFixture(t, active, admin)
	input := map[string]any{
		"action": "create", "requestId": token(), "supplierId": supplier,
		"lines": []map[string]any{{"itemId": item, "quantity": 2, "unitPrice": 103}},
	}
	orderID := purchasePost(t, active, admin, input, 200)["id"]

	destination := filepath.Join(dir, "candidate.sqlite")
	if err := backupERPDatabase(source, destination); err != nil {
		t.Fatal(err)
	}
	copyDB, err := openDB(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	candidate := &App{db: copyDB}
	if id := purchasePost(t, candidate, admin, input, 200)["id"]; id != orderID {
		t.Fatalf("snapshot lost the purchase retry result: %v", id)
	}
	purchasePost(t, candidate, admin, map[string]any{"action": "approve", "poId": orderID}, 200)
	var status string
	if err := db.QueryRow("SELECT status FROM erp_purchase_orders WHERE organization_id=? AND id=?", org, orderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("candidate mutation changed live order status to %s", status)
	}

	existing := filepath.Join(dir, "existing.sqlite")
	if err := os.WriteFile(existing, []byte("keep this file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := backupERPDatabase(source, existing); err == nil {
		t.Fatal("backup replaced an existing file")
	}
	if bytes, err := os.ReadFile(existing); err != nil || string(bytes) != "keep this file" {
		t.Fatalf("existing file changed: %v", err)
	}
}
