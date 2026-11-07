package main

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
)

const purchaseReturnPath = "/api/stations/erp/purchase-returns"

func TestPurchaseReturnPartialReplayAndConcurrentRemainder(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	office := login(t, a, "office@suite.test", space)
	viewer := login(t, a, "viewer@suite.test", space)
	foreign := login(t, a, "admin@suite.test", token())
	org, _, item, warehouse := shippingFixture(t, a, admin)
	var line string
	if err := a.db.QueryRow("SELECT id FROM erp_receipt_lines WHERE organization_id=? AND item_id=? AND quantity=2", org, item).Scan(&line); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"receiptLineId": {line}}
	if w := request(a, "GET", purchaseReturnPath+"?"+query.Encode(), nil, office...); w.Code != http.StatusOK {
		t.Fatalf("return availability returned %d: %s", w.Code, w.Body)
	}
	payload := func(key string, quantity int) map[string]any {
		return map[string]any{"receiptLineId": line, "quantity": quantity, "reason": "Supplier inspection failed", "requestId": key}
	}
	key := token()
	if got := request(a, "POST", purchaseReturnPath, payload(key, 1), viewer...).Code; got != 403 {
		t.Fatalf("read-only return returned %d", got)
	}
	if got := request(a, "POST", purchaseReturnPath, payload(key, 1), foreign...).Code; got != 404 {
		t.Fatalf("cross-tenant return returned %d", got)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_return_audit BEFORE INSERT ON audit_events WHEN NEW.action='erp.purchase_return' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if got := request(a, "POST", purchaseReturnPath, payload(key, 1), office...).Code; got != 503 {
		t.Fatalf("late audit failure returned %d", got)
	}
	if _, err := a.db.Exec("DROP TRIGGER fail_return_audit"); err != nil {
		t.Fatal(err)
	}
	first := request(a, "POST", purchaseReturnPath, payload(key, 1), office...)
	replay := request(a, "POST", purchaseReturnPath, payload(key, 1), office...)
	if first.Code != 200 || replay.Code != 200 || first.Body.String() != replay.Body.String() {
		t.Fatalf("partial return/replay returned %d/%d: %s %s", first.Code, replay.Code, first.Body, replay.Body)
	}
	if got := request(a, "POST", purchaseReturnPath, payload(key, 2), office...).Code; got != 409 {
		t.Fatalf("changed retry payload returned %d", got)
	}
	var quantity, cost int64
	if err := a.db.QueryRow("SELECT total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost); err != nil || quantity != 2 || cost != 201 {
		t.Fatalf("partial return costing: quantity=%d cost=%d error=%v", quantity, cost, err)
	}

	var wait sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- request(a, "POST", purchaseReturnPath, payload(token(), 1), office...).Code
		}()
	}
	wait.Wait()
	close(results)
	outcomes := map[int]int{}
	for status := range results {
		outcomes[status]++
	}
	if outcomes[200] != 1 || outcomes[409] != 1 {
		t.Fatalf("concurrent return outcomes: %v", outcomes)
	}
	if err := a.db.QueryRow("SELECT total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost); err != nil || quantity != 1 || cost != 100 {
		t.Fatalf("full return costing: quantity=%d cost=%d error=%v", quantity, cost, err)
	}
	var rows, units, value, audit, original int
	if err := a.db.QueryRow("SELECT count(*),coalesce(-sum(quantity_change),0),coalesce(-sum(total_value),0) FROM erp_stock_ledger WHERE organization_id=? AND warehouse_id=? AND item_id=? AND change_type='purchase_return'", org, warehouse, item).Scan(&rows, &units, &value); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=? AND action='erp.purchase_return' AND reason='Supplier inspection failed'", org).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT quantity FROM erp_receipt_lines WHERE id=?", line).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || units != 2 || value != 202 || audit != 2 || original != 2 {
		t.Fatalf("unexpected return state rows=%d units=%d value=%d audits=%d received=%d", rows, units, value, audit, original)
	}
}
