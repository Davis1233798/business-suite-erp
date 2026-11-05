package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

func billingPost(t *testing.T, a *App, cookies []*http.Cookie, payload map[string]any, status int) map[string]any {
	t.Helper()
	if payload["idempotencyKey"] == nil {
		payload["idempotencyKey"] = token()
	}
	w := request(a, "POST", "/api/stations/erp/finance", payload, cookies...)
	if w.Code != status {
		t.Fatalf("billing %v: expected %d, got %d %s", payload["action"], status, w.Code, w.Body)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}

func TestBillingConcurrentInvoiceAndPaymentNeverDuplicateOrOverpay(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, po := billingPurchase(t, a, admin, true)
	var wg sync.WaitGroup
	responses := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- request(a, "POST", "/api/stations/erp/finance", map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po, "idempotencyKey": token()}, admin...).Code
		}()
	}
	wg.Wait()
	close(responses)
	success, conflicts := 0, 0
	for code := range responses {
		if code == 200 {
			success++
		} else if code == 409 {
			conflicts++
		} else {
			t.Fatalf("unexpected invoice race status %d", code)
		}
	}
	if success != 1 || conflicts != 7 {
		t.Fatalf("invoice success=%d conflicts=%d", success, conflicts)
	}
	var invoice string
	if err := a.db.QueryRow("SELECT id FROM erp_invoices WHERE organization_id=? AND id LIKE 'invoice_%'", org).Scan(&invoice); err != nil {
		t.Fatal(err)
	}
	payments := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payments <- request(a, "POST", "/api/stations/erp/finance", map[string]any{"action": "payment", "invoiceId": invoice, "amount": 800, "method": "cash", "idempotencyKey": token()}, admin...).Code
		}()
	}
	wg.Wait()
	close(payments)
	success, conflicts = 0, 0
	for code := range payments {
		if code == 200 {
			success++
		} else if code == 409 {
			conflicts++
		} else {
			t.Fatalf("unexpected payment race status %d", code)
		}
	}
	var paid, count int
	if err := a.db.QueryRow("SELECT paid_amount FROM erp_invoices WHERE id=?", invoice).Scan(&paid); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM erp_payments WHERE invoice_id=?", invoice).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if success != 1 || conflicts != 1 || paid != 800 || count != 1 {
		t.Fatalf("payment success=%d conflicts=%d paid=%d rows=%d", success, conflicts, paid, count)
	}
}

func TestBillingSourceIntegrityDateAndActorBoundRetry(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org, po := billingPurchase(t, a, admin, true)
	billingPost(t, a, admin, map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po, "dueDate": "2035-02-30"}, 400)
	if _, err := a.db.Exec("UPDATE erp_purchase_orders SET total_amount=total_amount+1 WHERE organization_id=? AND id=?", org, po); err != nil {
		t.Fatal(err)
	}
	billingPost(t, a, admin, map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po}, 409)
	if _, err := a.db.Exec("UPDATE erp_purchase_orders SET total_amount=total_amount-1 WHERE organization_id=? AND id=?", org, po); err != nil {
		t.Fatal(err)
	}
	key := token()
	payload := map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po, "idempotencyKey": key}
	billingPost(t, a, admin, payload, 200)
	billingPost(t, a, login(t, a, "office@suite.test", space), payload, 409)
}

func billingPurchase(t *testing.T, a *App, admin []*http.Cookie, receive bool) (org, po string) {
	t.Helper()
	org, supplier, item, warehouse := purchaseFixture(t, a, admin)
	po = newPurchase(t, a, admin, supplier, item)
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po}, 200)
	if receive {
		var line string
		if err := a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po).Scan(&line); err != nil {
			t.Fatal(err)
		}
		purchasePost(t, a, admin, map[string]any{"action": "receive", "poId": po, "warehouseId": warehouse, "lines": []map[string]any{{"lineId": line, "quantity": 10}}}, 200)
	}
	return org, po
}

func TestBillingInvoiceReplayPartialPaymentAndSettlement(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, po := billingPurchase(t, a, admin, true)
	invoiceRequest := map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po, "dueDate": "2035-01-15", "idempotencyKey": token()}
	invoice := billingPost(t, a, admin, invoiceRequest, 200)
	id, valid := invoice["invoiceId"].(string)
	if !valid || id == "" {
		t.Fatal("missing persisted invoice identity")
	}
	if again := billingPost(t, a, admin, invoiceRequest, 200); again["invoiceId"] != id {
		t.Fatal("invoice replay changed identity")
	}
	invoiceRequest["dueDate"] = "2035-01-16"
	billingPost(t, a, admin, invoiceRequest, 409)
	billingPost(t, a, admin, map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po}, 409)
	var total int64
	if err := a.db.QueryRow("SELECT total_amount FROM erp_invoices WHERE organization_id=? AND id=?", org, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1200 {
		t.Fatalf("incorrect invoice total %d", total)
	}
	payment := map[string]any{"action": "payment", "invoiceId": id, "amount": 400, "method": "bank_transfer", "idempotencyKey": token()}
	first := billingPost(t, a, admin, payment, 200)
	if first["status"] != "partially_paid" {
		t.Fatal(first)
	}
	if again := billingPost(t, a, admin, payment, 200); again["paymentNumber"] != first["paymentNumber"] {
		t.Fatal("payment replay duplicated posting")
	}
	payment["amount"] = 401
	billingPost(t, a, admin, payment, 409)
	billingPost(t, a, admin, map[string]any{"action": "payment", "invoiceId": id, "amount": 801, "method": "cash"}, 409)
	billingPost(t, a, admin, map[string]any{"action": "payment", "invoiceId": id, "amount": 800, "method": "cash"}, 200)
	billingPost(t, a, admin, map[string]any{"action": "payment", "invoiceId": id, "amount": 1, "method": "cash"}, 409)
	var paid, payments int64
	var status string
	if err := a.db.QueryRow("SELECT paid_amount,status FROM erp_invoices WHERE id=?", id).Scan(&paid, &status); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM erp_payments WHERE organization_id=? AND invoice_id=?", org, id).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if paid != total || status != "paid" || payments != 2 {
		t.Fatalf("paid=%d status=%s payments=%d", paid, status, payments)
	}
}

func TestBillingAuthorizationSourceCompletionAndAuditRollback(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	_, po := billingPurchase(t, a, admin, false)
	payload := map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": po}
	billingPost(t, a, login(t, a, "front@suite.test", space), payload, 403)
	billingPost(t, a, login(t, a, "viewer@suite.test", space), payload, 403)
	billingPost(t, a, login(t, a, "admin@suite.test", token()), payload, 404)
	billingPost(t, a, admin, payload, 409)
	org, complete := billingPurchase(t, a, admin, true)
	if _, err := a.db.Exec("CREATE TRIGGER billing_audit_failure BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'billing audit failure'); END"); err != nil {
		t.Fatal(err)
	}
	key := token()
	billingPost(t, a, admin, map[string]any{"action": "invoice", "invoiceKind": "payable", "orderId": complete, "idempotencyKey": key}, 503)
	var invoices, retries int
	if err := a.db.QueryRow("SELECT count(*) FROM erp_invoices WHERE organization_id=? AND id LIKE 'invoice_%'", org).Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT count(*) FROM idempotency_keys WHERE organization_id=? AND key=?", org, key).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if invoices != 0 || retries != 0 {
		t.Fatalf("late failure leaked invoices=%d retries=%d", invoices, retries)
	}
}
