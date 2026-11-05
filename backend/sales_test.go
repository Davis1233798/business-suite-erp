package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
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

type salesState struct {
	status                            string
	shipped, balance                  int64
	ledgerQuantity, ledgerValue       int64
	costQuantity, cost, average       int64
	orders, shipments, shipmentLines  int
	ledgerRows, auditRows, replayRows int
}

func snapshotSalesState(t *testing.T, a *App, org, order, line, item, warehouse, requestID string) salesState {
	t.Helper()
	state := salesState{}
	queries := []struct {
		query string
		args  []any
		dest  any
	}{
		{"SELECT status FROM erp_sales_orders WHERE organization_id=? AND id=?", []any{org, order}, &state.status},
		{"SELECT shipped_quantity FROM erp_sales_order_lines WHERE organization_id=? AND id=?", []any{org, line}, &state.shipped},
		{"SELECT count(*) FROM erp_sales_orders WHERE organization_id=?", []any{org}, &state.orders},
		{"SELECT count(*) FROM erp_shipments WHERE organization_id=?", []any{org}, &state.shipments},
		{"SELECT count(*) FROM erp_shipment_lines WHERE organization_id=?", []any{org}, &state.shipmentLines},
		{"SELECT count(*) FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=?", []any{org, item, warehouse}, &state.ledgerRows},
		{"SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", []any{org, item, warehouse}, &state.balance},
		{"SELECT coalesce(sum(quantity_change),0),coalesce(sum(total_value),0) FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=?", []any{org, item, warehouse}, []any{&state.ledgerQuantity, &state.ledgerValue}},
		{"SELECT total_quantity,total_cost,moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", []any{org, item}, []any{&state.costQuantity, &state.cost, &state.average}},
		{"SELECT count(*) FROM audit_events WHERE organization_id=?", []any{org}, &state.auditRows},
		{"SELECT count(*) FROM idempotency_keys WHERE organization_id=? AND key=?", []any{org, requestID}, &state.replayRows},
	}
	for _, check := range queries {
		if values, ok := check.dest.([]any); ok {
			if err := a.db.QueryRow(check.query, check.args...).Scan(values...); err != nil {
				t.Fatal(err)
			}
		} else if err := a.db.QueryRow(check.query, check.args...).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	return state
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

func TestSalesLateAuditFailureRollsBackShipmentAndReplay(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, customer, item, warehouse := shippingFixture(t, a, admin)
	order, line := createSales(t, a, admin, customer, item)
	salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": order}, 200)
	requestID := "late-audit-rollback-" + token()
	before := snapshotSalesState(t, a, org, order, line, item, warehouse, requestID)
	if _, err := a.db.Exec(`CREATE TRIGGER reject_sales_audit BEFORE INSERT ON audit_events
		WHEN NEW.action='SHIP_SALES' BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	salesPost(t, a, admin, map[string]any{
		"action": "ship", "requestId": requestID, "soId": order, "warehouseId": warehouse,
		"lines": []map[string]any{{"lineId": line, "quantity": 1}},
	}, 500)
	after := snapshotSalesState(t, a, org, order, line, item, warehouse, requestID)
	if before != after {
		t.Fatalf("late audit failure left sales changes: before=%+v after=%+v", before, after)
	}
}

func TestSalesRejectsForeignReferencesAndDuplicateShipmentLines(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, customer, item, warehouse := shippingFixture(t, a, admin)
	foreignAdmin := login(t, a, "admin@suite.test", token())
	_, foreignCustomer, foreignItem, foreignWarehouse := shippingFixture(t, a, foreignAdmin)
	order, line := createSales(t, a, admin, customer, item)

	requestID := "foreign-customer-" + token()
	before := snapshotSalesState(t, a, org, order, line, item, warehouse, requestID)
	salesPost(t, a, admin, map[string]any{
		"action": "create", "requestId": requestID, "customerId": foreignCustomer,
		"lines": []map[string]any{{"itemId": item, "quantity": 1, "unitPrice": 10}},
	}, 400)
	if after := snapshotSalesState(t, a, org, order, line, item, warehouse, requestID); before != after {
		t.Fatalf("foreign customer changed local sales state: before=%+v after=%+v", before, after)
	}

	_, foreignLine := createSales(t, a, foreignAdmin, foreignCustomer, foreignItem)
	salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": order}, 200)
	for name, shipment := range map[string]map[string]any{
		"foreign warehouse": {"action": "ship", "soId": order, "warehouseId": foreignWarehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}}},
		"foreign line":      {"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": foreignLine, "quantity": 1}}},
		"duplicate line":    {"action": "ship", "soId": order, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}, {"lineId": line, "quantity": 1}}},
	} {
		requestID = name + "-" + token()
		shipment["requestId"] = requestID
		before = snapshotSalesState(t, a, org, order, line, item, warehouse, requestID)
		salesPost(t, a, admin, shipment, 400)
		if after := snapshotSalesState(t, a, org, order, line, item, warehouse, requestID); before != after {
			t.Fatalf("%s rejection changed local sales state: before=%+v after=%+v", name, before, after)
		}
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

func TestShipmentCostUpgradeRefusesOverflowWithoutChangingLegacySchemaOrRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overflow-erp.sqlite")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ALTER TABLE erp_shipment_lines DROP COLUMN extended_cost"); err != nil {
		t.Fatal(err)
	}
	const quantity, unitCost int64 = 2, 9007199254740991
	if _, err = db.Exec("INSERT INTO erp_shipment_lines(id,organization_id,shipment_id,item_id,quantity,unit_cost) VALUES('overflow-line','legacy-org','legacy-shipment','legacy-item',?,?)", quantity, unitCost); err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = migrateShipmentCost(tx)
	if err == nil || !strings.Contains(err.Error(), "migration refused") {
		t.Fatalf("overflow migration error = %v", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("PRAGMA table_info(erp_shipment_lines)")
	if err != nil {
		t.Fatal(err)
	}
	hasExtendedCost := false
	for rows.Next() {
		var ordinal, required, primary int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&ordinal, &name, &kind, &required, &defaultValue, &primary); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		hasExtendedCost = hasExtendedCost || name == "extended_cost"
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if hasExtendedCost {
		t.Fatal("failed migration changed the legacy schema")
	}
	var gotQuantity, gotCost int64
	if err = db.QueryRow("SELECT quantity,unit_cost FROM erp_shipment_lines WHERE id='overflow-line'").Scan(&gotQuantity, &gotCost); err != nil || gotQuantity != quantity || gotCost != unitCost {
		t.Fatalf("failed migration changed legacy shipment row: quantity=%d cost=%d err=%v", gotQuantity, gotCost, err)
	}
}
