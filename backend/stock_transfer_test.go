package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func transferPost(t *testing.T, a *App, cookies []*http.Cookie, payload map[string]any, status int) map[string]any {
	t.Helper()
	if payload["requestId"] == nil {
		payload["requestId"] = token()
	}
	w := request(a, "POST", "/api/stations/erp/inventory", payload, cookies...)
	if w.Code != status {
		t.Fatalf("transfer expected%d got%d %s", status, w.Code, w.Body)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}

func TestStockTransferConservesQuantityCostAndReplays(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, _, item, source := shippingFixture(t, a, admin)
	destination := "warehouse_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Transfer destination',?)", destination, org, destination); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"action": "transfer", "itemId": item, "fromWarehouseId": source, "toWarehouseId": destination, "quantity": 2, "requestId": token()}
	first := transferPost(t, a, admin, input, 200)
	if first["transferId"] == nil {
		t.Fatal("missing transfer reference")
	}
	if again := transferPost(t, a, admin, input, 200); again["transferId"] != first["transferId"] {
		t.Fatal("replay created another transfer")
	}
	input["quantity"] = 1
	transferPost(t, a, admin, input, 409)
	var sourceBalance, destinationBalance, quantity, cost, average int64
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&sourceBalance); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, destination).Scan(&destinationBalance); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT total_quantity,total_cost,moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost, &average); err != nil {
		t.Fatal(err)
	}
	var rows int
	var movedQuantity, movedValue int64
	if err := a.db.QueryRow("SELECT count(*),coalesce(sum(quantity_change),0),coalesce(sum(total_value),0) FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND change_type IN ('transfer_out','transfer_in')", org, item).Scan(&rows, &movedQuantity, &movedValue); err != nil {
		t.Fatal(err)
	}
	if sourceBalance != 1 || destinationBalance != 2 || quantity != 3 || cost != 302 || average != 101 || rows != 2 || movedQuantity != 0 || movedValue != 0 {
		t.Fatalf("balances=%d/%d cost=%d/%d/%d rows=%d deltas=%d/%d", sourceBalance, destinationBalance, quantity, cost, average, rows, movedQuantity, movedValue)
	}
	transferPost(t, a, admin, map[string]any{"action": "transfer", "itemId": item, "fromWarehouseId": source, "toWarehouseId": destination, "quantity": 2}, 409)
}

func TestStockTransferAuthorizationAndSecondLedgerRollback(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org, _, item, source := shippingFixture(t, a, admin)
	destination := "warehouse_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Transfer destination',?)", destination, org, destination); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"action": "transfer", "itemId": item, "fromWarehouseId": source, "toWarehouseId": destination, "quantity": 2, "requestId": token()}
	for _, email := range []string{"front@suite.test", "viewer@suite.test"} {
		transferPost(t, a, login(t, a, email, space), input, 403)
	}
	transferPost(t, a, login(t, a, "admin@suite.test", token()), input, 404)
	var beforeLedger, beforeAudit int
	if err := a.db.QueryRow("SELECT count(*) FROM erp_stock_ledger WHERE organization_id=?", org).Scan(&beforeLedger); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=?", org).Scan(&beforeAudit); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("CREATE TRIGGER transfer_late_failure BEFORE INSERT ON erp_stock_ledger WHEN NEW.change_type='transfer_in' BEGIN SELECT RAISE(ABORT,'destination failure'); END"); err != nil {
		t.Fatal(err)
	}
	transferPost(t, a, admin, input, 503)
	var ledger, audits, retries int
	if err := a.db.QueryRow("SELECT count(*) FROM erp_stock_ledger WHERE organization_id=?", org).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=?", org).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM idempotency_keys WHERE organization_id=? AND key=?", org, input["requestId"]).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if ledger != beforeLedger || audits != beforeAudit || retries != 0 {
		t.Fatalf("rollback ledger=%d/%d audits=%d/%d retries=%d", ledger, beforeLedger, audits, beforeAudit, retries)
	}
	if _, err := a.db.Exec("DROP TRIGGER transfer_late_failure"); err != nil {
		t.Fatal(err)
	}
	transferPost(t, a, admin, input, 200)
}

func TestConcurrentStockTransfersCannotOverdrawSource(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, _, item, source := shippingFixture(t, a, admin)
	destination := "warehouse_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Concurrent destination',?)", destination, org, destination); err != nil {
		t.Fatal(err)
	}
	var balance int64
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	want := balance / 2
	if want < 1 {
		t.Fatal("fixture source stock is too small for contention test")
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			payload, _ := json.Marshal(map[string]any{"action": "transfer", "itemId": item, "fromWarehouseId": source, "toWarehouseId": destination, "quantity": want, "requestId": token()})
			req, _ := http.NewRequest(http.MethodPost, "/api/stations/erp/inventory", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			for _, c := range admin {
				req.AddCookie(c)
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, req)
			statuses <- w.Code
		}()
	}
	close(start)
	group.Wait()
	close(statuses)
	var successes, conflicts int
	for status := range statuses {
		if status == 200 {
			successes++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatalf("concurrent transfer status %d", status)
		}
	}
	if successes != 2 && !(successes == 1 && conflicts == 1) {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	var sourceAfter, destAfter int64
	_ = a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&sourceAfter)
	_ = a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, destination).Scan(&destAfter)
	if sourceAfter < 0 || sourceAfter+destAfter != balance {
		t.Fatalf("source=%d destination=%d initial=%d", sourceAfter, destAfter, balance)
	}
}
