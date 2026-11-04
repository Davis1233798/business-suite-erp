package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func purchaseFixture(t *testing.T, a *App, c []*http.Cookie) (org, supplier, item, warehouse string) {
	t.Helper()
	org = journalOrg(t, a, c)
	for table, dest := range map[string]*string{"erp_suppliers": &supplier, "erp_items": &item, "erp_warehouses": &warehouse} {
		if e := a.db.QueryRow("SELECT id FROM "+table+" WHERE organization_id=? LIMIT 1", org).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	return
}
func purchasePost(t *testing.T, a *App, c []*http.Cookie, p map[string]any, status int) map[string]any {
	t.Helper()
	if p["requestId"] == nil {
		p["requestId"] = token()
	}
	w := request(a, "POST", "/api/stations/erp/purchases", p, c...)
	if w.Code != status {
		t.Fatalf("%v: %d %s", p["action"], w.Code, w.Body)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	return result.Data
}
func newPurchase(t *testing.T, a *App, c []*http.Cookie, supplier, item string) string {
	t.Helper()
	result := purchasePost(t, a, c, map[string]any{"action": "create", "supplierId": supplier, "lines": []map[string]any{{"itemId": item, "quantity": 10, "unitPrice": 120}}}, 200)
	return result["id"].(string)
}
func TestPurchaseApprovalReceiptReplayAndCosting(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	operator := login(t, a, "office@suite.test", space)
	org, supplier, item, warehouse := purchaseFixture(t, a, admin)
	po := newPurchase(t, a, operator, supplier, item)
	purchasePost(t, a, operator, map[string]any{"action": "approve", "poId": po}, 403)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 200)
	var line string
	a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&line)
	var beforeQty, beforeCost int64
	_ = a.db.QueryRow("SELECT total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&beforeQty, &beforeCost)
	key := token()
	p := map[string]any{"action": "receive", "requestId": key, "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 4}}}
	first := purchasePost(t, a, operator, p, 200)
	again := purchasePost(t, a, operator, p, 200)
	if first["id"] != again["id"] {
		t.Fatal("replay duplicated receipt")
	}
	p["lines"] = []map[string]any{{"lineId": line, "quantity": 5}}
	purchasePost(t, a, operator, p, 409)
	var received int
	a.db.QueryRow("SELECT received_quantity FROM erp_purchase_order_lines WHERE id=?", line).Scan(&received)
	if received != 4 {
		t.Fatal(received)
	}
	var qty, cost, average int64
	a.db.QueryRow("SELECT total_quantity,total_cost,moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&qty, &cost, &average)
	if qty != beforeQty+4 || cost != beforeCost+480 || average != (cost+qty/2)/qty {
		t.Fatalf("costing %d %d %d", qty, cost, average)
	}
	purchasePost(t, a, operator, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 6}}}, 200)
	var status string
	a.db.QueryRow("SELECT status FROM erp_purchase_orders WHERE id=?", po).Scan(&status)
	if status != "received" {
		t.Fatal(status)
	}
	var audits int
	a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=? AND entity_id=?", org, po).Scan(&audits)
	if audits != 4 {
		t.Fatal("missing lifecycle audit", audits)
	}
}
func TestPurchasePermissionsForeignIDsAndAtomicFailure(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org, supplier, item, warehouse := purchaseFixture(t, a, admin)
	for _, email := range []string{"viewer@suite.test", "front@suite.test"} {
		c := login(t, a, email, space)
		purchasePost(t, a, c, map[string]any{"action": "create", "supplierId": supplier, "lines": []map[string]any{{"itemId": item, "quantity": 10, "unitPrice": 120}}}, 403)
	}
	other := login(t, a, "admin@suite.test", token())
	_, foreignSupplier, _, foreignWarehouse := purchaseFixture(t, a, other)
	purchasePost(t, a, admin, map[string]any{"action": "create", "supplierId": foreignSupplier, "lines": []map[string]any{{"itemId": item, "quantity": 10, "unitPrice": 120}}}, 400)
	po := newPurchase(t, a, admin, supplier, item)
	var line string
	a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&line)
	p := map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}}}
	purchasePost(t, a, admin, p, 409)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 200)
	p["requestId"] = token()
	p["warehouseId"] = foreignWarehouse
	purchasePost(t, a, admin, p, 400)
	p["requestId"] = token()
	p["warehouseId"] = warehouse
	if _, e := a.db.Exec("CREATE TRIGGER reject_purchase_cost BEFORE UPDATE ON erp_item_costing BEGIN SELECT RAISE(ABORT,'late cost failure'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := a.db.Exec("CREATE TRIGGER reject_purchase_cost_insert BEFORE INSERT ON erp_item_costing BEGIN SELECT RAISE(ABORT,'late cost failure'); END"); e != nil {
		t.Fatal(e)
	}
	purchasePost(t, a, admin, p, 500)
	var received, receipts int
	a.db.QueryRow("SELECT received_quantity FROM erp_purchase_order_lines WHERE id=?", line).Scan(&received)
	a.db.QueryRow("SELECT count(*) FROM erp_receipts WHERE organization_id=? AND po_id=?", org, po).Scan(&receipts)
	if received != 0 || receipts != 0 {
		t.Fatal("partial receipt survived", received, receipts)
	}
}
func TestConcurrentPurchaseReceiptsCannotOverReceive(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	_, supplier, item, warehouse := purchaseFixture(t, a, admin)
	po := newPurchase(t, a, admin, supplier, item)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 200)
	var line string
	a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&line)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(a, "POST", "/api/stations/erp/purchases", map[string]any{"action": "receive", "requestId": token(), "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 7}}}, admin...)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	var success, conflict int
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatal(fmt.Sprint(code))
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}

func TestPurchaseBoxesAndForeignLines(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	_, supplier, item, warehouse := purchaseFixture(t, a, admin)
	if _, e := a.db.Exec("UPDATE erp_items SET box_conversion_ratio=5 WHERE id=?", item); e != nil {
		t.Fatal(e)
	}
	po := newPurchase(t, a, admin, supplier, item)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 200)
	var line string
	a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&line)
	other := login(t, a, "admin@suite.test", token())
	_, otherSupplier, otherItem, _ := purchaseFixture(t, a, other)
	foreignPO := newPurchase(t, a, other, otherSupplier, otherItem)
	var foreignLine string
	a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", foreignPO).Scan(&foreignLine)
	purchasePost(t, a, admin, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": foreignLine, "quantity": 1}}}, 400)
	purchasePost(t, a, admin, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 1}, {"lineId": line, "quantity": 1}}}, 400)
	purchasePost(t, a, admin, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 3, "isBoxes": true}}}, 409)
	purchasePost(t, a, admin, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 2, "isBoxes": true}}}, 200)
	var received int
	a.db.QueryRow("SELECT received_quantity FROM erp_purchase_order_lines WHERE id=?", line).Scan(&received)
	if received != 10 {
		t.Fatal("box conversion not persisted", received)
	}
}
func TestPurchaseRejectAndInvalidInput(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	_, supplier, item, _ := purchaseFixture(t, a, admin)
	purchasePost(t, a, nil, map[string]any{"action": "create"}, 401)
	for _, line := range []map[string]any{{"itemId": item, "quantity": 0, "unitPrice": 1}, {"itemId": item, "quantity": 1, "unitPrice": -1}, {"itemId": item, "quantity": 1.5, "unitPrice": 1}} {
		purchasePost(t, a, admin, map[string]any{"action": "create", "supplierId": supplier, "lines": []map[string]any{line}}, 400)
	}
	po := newPurchase(t, a, admin, supplier, item)
	purchasePost(t, a, admin, map[string]any{"action": "reject", "poId": po, "reason": " "}, 400)
	purchasePost(t, a, admin, map[string]any{"action": "reject", "poId": po, "reason": "Budget approval required"}, 200)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 409)
	var reason, status string
	a.db.QueryRow("SELECT status,rejected_reason FROM erp_purchase_orders WHERE id=?", po).Scan(&status, &reason)
	if status != "rejected" || reason != "Budget approval required" {
		t.Fatal("rejection not persisted", status, reason)
	}
}
