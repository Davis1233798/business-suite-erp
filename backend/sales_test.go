package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
)

func salesPost(t *testing.T, a *App, cookies []*http.Cookie, payload map[string]any, status int) map[string]any {
	t.Helper()
	if payload["requestId"] == nil {
		payload["requestId"] = token()
	}
	w := request(a, "POST", "/api/stations/erp/sales", payload, cookies...)
	if w.Code != status {
		t.Fatalf("sales %v: got %d: %s", payload["action"], w.Code, w.Body)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}

// Real purchase receipts provide the inventory consumed by the sales workflow.
func shippingFixture(t *testing.T, a *App, cookies []*http.Cookie) (org, customer, item, warehouse string) {
	t.Helper()
	org = journalOrg(t, a, cookies)
	var supplier string
	if err := a.db.QueryRow("SELECT id FROM erp_suppliers WHERE organization_id=? AND is_active=1 LIMIT 1", org).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT id FROM erp_customers WHERE organization_id=? AND is_active=1 LIMIT 1", org).Scan(&customer); err != nil {
		t.Fatal(err)
	}
	item, warehouse = "item_"+token(), "warehouse_"+token()
	if _, err := a.db.Exec("INSERT INTO erp_items(id,organization_id,sku,name) VALUES(?,?,?,'Shipment item')", item, org, item); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Shipment warehouse',?)", warehouse, org, warehouse); err != nil {
		t.Fatal(err)
	}
	for _, line := range []map[string]any{{"itemId": item, "quantity": 1, "unitPrice": 100}, {"itemId": item, "quantity": 2, "unitPrice": 101}} {
		created := purchasePost(t, a, cookies, map[string]any{"action": "create", "supplierId": supplier, "lines": []map[string]any{line}}, 200)
		po := created["id"].(string)
		purchasePost(t, a, cookies, map[string]any{"action": "approve", "poId": po}, 200)
		var lineID string
		if err := a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&lineID); err != nil {
			t.Fatal(err)
		}
		purchasePost(t, a, cookies, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": lineID, "quantity": line["quantity"]}}}, 200)
	}
	return
}

func createSales(t *testing.T, a *App, cookies []*http.Cookie, customer, item string) (order, line string) {
	t.Helper()
	result := salesPost(t, a, cookies, map[string]any{"action": "create", "customerId": customer, "lines": []map[string]any{{"itemId": item, "quantity": 3, "unitPrice": 300}}}, 200)
	order = result["id"].(string)
	if err := a.db.QueryRow("SELECT id FROM erp_sales_order_lines WHERE so_id=?", order).Scan(&line); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSalesPartialFullReplayAndExactCost(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	operator := login(t, a, "office@suite.test", space)
	org, customer, item, warehouse := shippingFixture(t, a, admin)
	order, line := createSales(t, a, operator, customer, item)
	ship := map[string]any{"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 2}}}
	salesPost(t, a, operator, ship, 409)
	salesPost(t, a, operator, map[string]any{"action": "confirm", "soId": order}, 200)
	ship["requestId"] = token()
	first := salesPost(t, a, operator, ship, 200)
	if again := salesPost(t, a, operator, ship, 200); again["id"] != first["id"] {
		t.Fatal("replay deducted stock twice")
	}
	ship["lines"] = []map[string]any{{"lineId": line, "quantity": 1}}
	salesPost(t, a, operator, ship, 409)
	ship["requestId"] = token()
	salesPost(t, a, operator, ship, 200)
	var status string
	var quantity, cost int64
	if err := a.db.QueryRow("SELECT status FROM erp_sales_orders WHERE id=?", order).Scan(&status); err != nil || status != "shipped" {
		t.Fatal(status, err)
	}
	if err := a.db.QueryRow("SELECT total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost); err != nil || quantity != 0 || cost != 0 {
		t.Fatal("cost not depleted exactly", quantity, cost, err)
	}
	var extended, ledger int64
	if err := a.db.QueryRow("SELECT sum(extended_cost) FROM erp_shipment_lines WHERE organization_id=? AND item_id=?", org, item).Scan(&extended); err != nil || extended != 302 {
		t.Fatal("exact shipment cost", extended, err)
	}
	if err := a.db.QueryRow("SELECT sum(total_value) FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND change_type='sales_shipment'", org, item).Scan(&ledger); err != nil || ledger != -302 {
		t.Fatal("stock cost does not reconcile", ledger, err)
	}
	salesPost(t, a, operator, map[string]any{"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}}}, 409)
}

func TestSalesAuthorizationAndWarehouseStock(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org, customer, item, warehouse := shippingFixture(t, a, admin)
	salesPost(t, a, nil, map[string]any{"action": "create"}, 401)
	for _, email := range []string{"viewer@suite.test", "front@suite.test"} {
		salesPost(t, a, login(t, a, email, space), map[string]any{"action": "create"}, 403)
	}
	order, line := createSales(t, a, admin, customer, item)
	salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": order}, 200)
	empty := "warehouse_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Empty warehouse',?)", empty, org, empty); err != nil {
		t.Fatal(err)
	}
	salesPost(t, a, admin, map[string]any{"action": "ship", "soId": order, "warehouseId": empty, "lines": []map[string]any{{"lineId": line, "quantity": 1}}}, 409)
	salesPost(t, a, admin, map[string]any{"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 4}}}, 409)
	other := login(t, a, "admin@suite.test", token())
	salesPost(t, a, other, map[string]any{"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}}}, 404)
}

func TestConcurrentSalesCannotOversell(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	_, customer, item, warehouse := shippingFixture(t, a, admin)
	var orders, lines [2]string
	for i := range orders {
		orders[i], lines[i] = createSales(t, a, admin, customer, item)
		salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": orders[i]}, 200)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := request(a, "POST", "/api/stations/erp/sales", map[string]any{"action": "ship", "requestId": token(), "soId": orders[i], "warehouseId": warehouse, "lines": []map[string]any{{"lineId": lines[i], "quantity": 2}}}, admin...)
			codes <- w.Code
		}(i)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatal("unexpected shipment status", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent stock claim", success, conflict)
	}
}

func TestShipmentExtendedCostUpgradePreservesExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-erp.sqlite")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the prior repository schema, whether the new code created the column or not.
	rows, err := db.Query("PRAGMA table_info(erp_shipment_lines)")
	if err != nil {
		t.Fatal(err)
	}
	hasColumn := false
	for rows.Next() {
		var ordinal, required, primary int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&ordinal, &name, &kind, &required, &defaultValue, &primary); err != nil {
			t.Fatal(err)
		}
		if name == "extended_cost" {
			hasColumn = true
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if hasColumn {
		if _, err = db.Exec("ALTER TABLE erp_shipment_lines DROP COLUMN extended_cost"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO erp_shipment_lines(id,organization_id,shipment_id,item_id,quantity,unit_cost) VALUES('old-line','old-org','old-shipment','old-item',3,17)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var cost int64
	if err = db.QueryRow("SELECT extended_cost FROM erp_shipment_lines WHERE id='old-line'").Scan(&cost); err != nil || cost != 51 {
		t.Fatal("legacy cost migration", cost, err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM erp_shipment_lines WHERE id='old-line'").Scan(&count); err != nil || count != 1 {
		t.Fatal("legacy row changed", count, err)
	}
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("migrated database integrity", integrity, err)
	}
}
