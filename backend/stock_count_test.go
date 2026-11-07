package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"
)

const stockCountPath = "/api/stations/erp/stock-count"

func stockCountSnapshot(t *testing.T, a *App, cookies []*http.Cookie, item, warehouse string) string {
	t.Helper()
	query := url.Values{"itemId": {item}, "warehouseId": {warehouse}}
	w := request(a, "GET", stockCountPath+"?"+query.Encode(), nil, cookies...)
	if w.Code != 200 {
		t.Fatalf("stock count snapshot returned %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Quantity int64  `json:"quantity"`
			Version  string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.Quantity != 3 || result.Data.Version == "" {
		t.Fatalf("expected a versioned snapshot of three received units: %s", w.Body.String())
	}
	return result.Data.Version
}

func TestStockCountPersistsExactAdjustmentAndReplay(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	office := login(t, a, "office@suite.test", space)
	other := login(t, a, "admin@suite.test", token())
	org, _, item, warehouse := shippingFixture(t, a, admin)
	version := stockCountSnapshot(t, a, admin, item, warehouse)
	input := map[string]any{
		"itemId": item, "warehouseId": warehouse, "expectedQuantity": 3, "expectedVersion": version,
		"countedQuantity": 2, "reason": "One unit missing from the bin", "idempotencyKey": token(),
	}
	for _, check := range []struct {
		cookies []*http.Cookie
		want    int
	}{{office, 403}, {other, 404}} {
		if got := request(a, "POST", stockCountPath, input, check.cookies...); got.Code != check.want {
			t.Fatalf("stock count access returned %d, expected %d", got.Code, check.want)
		}
	}
	first := request(a, "POST", stockCountPath, input, admin...)
	replay := request(a, "POST", stockCountPath, input, admin...)
	if first.Code != 200 || replay.Code != 200 || first.Body.String() != replay.Body.String() {
		t.Fatalf("count/replay returned %d/%d: %s %s", first.Code, replay.Code, first.Body, replay.Body)
	}
	input["countedQuantity"] = 1
	if got := request(a, "POST", stockCountPath, input, admin...).Code; got != 409 {
		t.Fatalf("changed request returned %d", got)
	}

	var quantity, cost, average, count, value, balance int64
	if err := a.db.QueryRow("SELECT total_quantity,total_cost,moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost, &average); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*),coalesce(sum(total_value),0),coalesce(max(balance_after),0) FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? AND change_type='adjustment'", org, item, warehouse).Scan(&count, &value, &balance); err != nil {
		t.Fatal(err)
	}
	if quantity != 2 || cost != 201 || average != 101 || count != 1 || value != -101 || balance != 2 {
		t.Fatalf("unexpected count costing qty=%d cost=%d average=%d rows=%d value=%d balance=%d", quantity, cost, average, count, value, balance)
	}
}

func TestStockCountRollsBackAuditFailureAndRejectsStaleConcurrentWrite(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, _, item, warehouse := shippingFixture(t, a, admin)
	version := stockCountSnapshot(t, a, admin, item, warehouse)
	input := func(key string) map[string]any {
		return map[string]any{
			"itemId": item, "warehouseId": warehouse, "expectedQuantity": 3, "expectedVersion": version,
			"countedQuantity": 4, "reason": "One extra unit in the bin", "idempotencyKey": key,
		}
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_count_audit BEFORE INSERT ON audit_events WHEN NEW.action='erp.stock_count' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	retryKey := token()
	if got := request(a, "POST", stockCountPath, input(retryKey), admin...).Code; got != 503 {
		t.Fatalf("late audit failure returned %d", got)
	}
	if current := stockCountSnapshot(t, a, admin, item, warehouse); current != version {
		t.Fatal("failed count changed the ledger version")
	}
	if _, err := a.db.Exec("DROP TRIGGER fail_count_audit"); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	results := make(chan int, 2)
	for _, key := range []string{retryKey, token()} {
		wait.Add(1)
		go func(key string) {
			defer wait.Done()
			results <- request(a, "POST", stockCountPath, input(key), admin...).Code
		}(key)
	}
	wait.Wait()
	close(results)
	outcomes := map[int]int{}
	for status := range results {
		outcomes[status]++
	}
	if outcomes[200] != 1 || outcomes[409] != 1 {
		t.Fatalf("concurrent count outcomes: %v", outcomes)
	}
	var quantity, cost, audited int64
	if err := a.db.QueryRow("SELECT total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&quantity, &cost); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=? AND action='erp.stock_count' AND reason='One extra unit in the bin'", org).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if quantity != 4 || cost != 403 || audited != 1 {
		t.Fatalf("unexpected committed state quantity=%d cost=%d audits=%d", quantity, cost, audited)
	}
}
