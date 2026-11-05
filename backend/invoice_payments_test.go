package main

import (
	"encoding/json"
	"net/http"
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
