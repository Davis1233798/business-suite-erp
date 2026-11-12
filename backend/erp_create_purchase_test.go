package main

import (
	"encoding/json"
	"testing"
)

func TestERPPurchaseCreation(t *testing.T) {
	a := appForTest(t)
	space := token()
	org := "org_test_" + space
	office := loginSite(t, a, "office@suite.test", space, "erp")
	front := loginSite(t, a, "front@suite.test", space, "erp")
	other := loginSite(t, a, "admin@suite.test", token(), "erp")
	var item, supplier string
	if e := a.db.QueryRow("SELECT id FROM erp_items WHERE organization_id=? LIMIT 1", org).Scan(&item); e != nil {
		t.Fatal(e)
	}
	if e := a.db.QueryRow("SELECT id FROM erp_suppliers WHERE organization_id=? LIMIT 1", org).Scan(&supplier); e != nil {
		t.Fatal(e)
	}
	create := map[string]any{"supplierId": supplier, "lines": []any{map[string]any{"itemId": item, "quantity": 10, "unitPrice": 100}}, "idempotencyKey": "purchase-create-0001"}
	if w := request(a, "POST", "/api/stations/erp/purchase-create", create, front...); w.Code != 403 {
		t.Fatalf("front create %d", w.Code)
	}
	if w := request(a, "POST", "/api/stations/erp/purchase-create", create, other...); w.Code != 404 {
		t.Fatalf("foreign supplier %d", w.Code)
	}
	w := request(a, "POST", "/api/stations/erp/purchase-create", create, office...)
	if w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			POID  string `json:"poId"`
			Total int64  `json:"total"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &created); e != nil || created.Data.POID == "" || created.Data.Total != 1000 {
		t.Fatalf("create result %+v %v", created, e)
	}
	if w := request(a, "POST", "/api/stations/erp/purchase-create", create, office...); w.Code != 200 {
		t.Fatalf("create replay %d", w.Code)
	}
}
