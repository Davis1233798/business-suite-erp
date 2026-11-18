package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Helper to post stock count adjustment
func stockCountPostForTest(t *testing.T, a *App, cookies []*http.Cookie, payload map[string]any, status int) map[string]any {
	t.Helper()
	if payload["idempotencyKey"] == nil {
		payload["idempotencyKey"] = token()
	}
	w := request(a, "POST", "/api/stations/erp/stock-count", payload, cookies...)
	if w.Code != status {
		t.Fatalf("stock count: expected %d, got %d %s", status, w.Code, w.Body)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Data
	}
	return nil
}

// 1. 非管理者拒絕：關帳僅限管理員 (admin)，經辦員 (operator)、檢視者 (viewer) 與未認證者均應拒絕
func TestFiscalPeriodClose_NonAdminRejection(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	operator := login(t, a, "office@suite.test", space)
	viewer := login(t, a, "viewer@suite.test", space)
	org := journalOrg(t, a, admin)

	period := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,0)",
		period, org, "P-2027-02", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}
	closePath := "/api/stations/erp/fiscal-periods/" + period + "/close"

	// Anonymous => 401
	if w := request(a, "POST", closePath, nil); w.Code != 401 {
		t.Fatalf("anonymous close expected 401, got %d", w.Code)
	}

	// Viewer => 403
	if w := request(a, "POST", closePath, nil, viewer...); w.Code != 403 {
		t.Fatalf("viewer close expected 403, got %d", w.Code)
	}

	// Operator => 200 (經辦員具備後台過帳與關帳權限)
	if w := request(a, "POST", closePath, nil, operator...); w.Code != 200 {
		t.Fatalf("operator close expected 200, got %d %s", w.Code, w.Body)
	}

	// 驗證狀態已更新為 closed
	var isClosed int
	if err := a.db.QueryRow("SELECT is_closed FROM erp_fiscal_periods WHERE id=?", period).Scan(&isClosed); err != nil || isClosed != 1 {
		t.Fatalf("expected period to be closed, got %d (err: %v)", isClosed, err)
	}

	// Admin => 200 (管理員亦可正常關帳另一個期間)
	periodAdmin := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,0)",
		periodAdmin, org, "P-2027-03", "2027-03-01", "2027-03-31"); err != nil {
		t.Fatal(err)
	}
	closeAdminPath := "/api/stations/erp/fiscal-periods/" + periodAdmin + "/close"
	if w := request(a, "POST", closeAdminPath, nil, admin...); w.Code != 200 {
		t.Fatalf("admin close expected 200, got %d %s", w.Code, w.Body)
	}
}

// 2. 關帳期間寫入拒絕 409：傳票、採購發票、付款、庫存盤點調整在已關帳期間均應拒絕 409
// 同時測試跨時區日期邊界防護 (台北時間 UTC+8 與 UTC 邊界檢查)
func TestFiscalPeriodClose_ClosedPeriodWriteRejection409(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org := journalOrg(t, a, admin)

	// 建立並關閉 2027-02 會計期間 (2027-02-01 ~ 2027-02-28)
	period := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,1)",
		period, org, "P-2027-02", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}

	// (A) 傳票寫入關帳期間 => 409
	closedJe := journalInput()
	closedJe["entryDate"] = "2027-02-15"
	if w := request(a, "POST", "/api/stations/erp/journal", closedJe, admin...); w.Code != 409 {
		t.Fatalf("journal in closed period expected 409, got %d %s", w.Code, w.Body)
	}

	// 傳票寫入未關帳期間 => 200
	openJe := journalInput()
	openJe["entryDate"] = "2027-03-01"
	if w := request(a, "POST", "/api/stations/erp/journal", openJe, admin...); w.Code != 200 {
		t.Fatalf("journal in open period expected 200, got %d %s", w.Code, w.Body)
	}

	// (B) 跨時區日期邊界防護：
	// 2027-01-31T16:30:00Z 在 UTC 為 1月31日，但在台北時間 (UTC+8) 為 2027-02-01 00:30:00 (落在已關帳二月期間)
	tzJe1 := journalInput()
	tzJe1["entryDate"] = "2027-01-31T16:30:00Z"
	if w := request(a, "POST", "/api/stations/erp/journal", tzJe1, admin...); w.Code != 409 {
		t.Fatalf("cross-timezone entry (Taipei Feb 1) in closed period expected 409, got %d %s", w.Code, w.Body)
	}

	// 2027-02-28T16:30:00Z 在 UTC 為 2027-02-28 (二月最後一天)，雖在台北為 3月1日，但 UTC 落於關帳期間內
	tzJe2 := journalInput()
	tzJe2["entryDate"] = "2027-02-28T16:30:00Z"
	if w := request(a, "POST", "/api/stations/erp/journal", tzJe2, admin...); w.Code != 409 {
		t.Fatalf("cross-timezone entry (UTC Feb 28) in closed period expected 409, got %d %s", w.Code, w.Body)
	}

	// (C) 採購發票寫入關帳期間 => 409
	// 準備採購訂單並完成驗收入庫
	_, po := billingPurchase(t, a, admin, true)
	invClosedPayload := map[string]any{
		"action":         "invoice",
		"invoiceKind":    "payable",
		"orderId":        po,
		"dueDate":        "2027-02-15",
		"date":           "2027-02-15",
		"idempotencyKey": token(),
	}
	wInv := request(a, "POST", "/api/stations/erp/finance", invClosedPayload, admin...)
	if wInv.Code != 409 {
		t.Fatalf("invoice in closed period expected 409, got %d %s", wInv.Code, wInv.Body)
	}

	// 在未關帳期間建立發票 => 200
	invOpenPayload := map[string]any{
		"action":         "invoice",
		"invoiceKind":    "payable",
		"orderId":        po,
		"dueDate":        "2027-03-15",
		"date":           "2027-03-15",
		"idempotencyKey": token(),
	}
	invOpen := billingPost(t, a, admin, invOpenPayload, 200)
	invoiceID := invOpen["invoiceId"].(string)

	// (D) 付款寫入關帳期間 => 409
	payClosedPayload := map[string]any{
		"action":         "payment",
		"invoiceId":      invoiceID,
		"amount":         100,
		"method":         "bank_transfer",
		"date":           "2027-02-20",
		"idempotencyKey": token(),
	}
	wPay := request(a, "POST", "/api/stations/erp/finance", payClosedPayload, admin...)
	if wPay.Code != 409 {
		t.Fatalf("payment in closed period expected 409, got %d %s", wPay.Code, wPay.Body)
	}

	// (E) 庫存盤點調整寫入關帳期間 => 409
	_, _, item, warehouse := shippingFixture(t, a, admin)
	countSnapshot := stockCountSnapshot(t, a, admin, item, warehouse)
	countClosedPayload := map[string]any{
		"itemId":           item,
		"warehouseId":      warehouse,
		"expectedQuantity": 3,
		"expectedVersion":  countSnapshot,
		"countedQuantity":  5,
		"reason":           "關帳期間盤點調整測試",
		"date":             "2027-02-10",
		"idempotencyKey":   token(),
	}
	wCount := request(a, "POST", "/api/stations/erp/stock-count", countClosedPayload, admin...)
	if wCount.Code != 409 {
		t.Fatalf("stock count adjustment in closed period expected 409, got %d %s", wCount.Code, wCount.Body)
	}
}

// 3. 負庫存出庫拒絕 409：出庫與調撥交易嚴格禁止負庫存，原子檢查當前庫存量
func TestNegativeInventory_OutboundAndTransferRejection409(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, customer, item, warehouse := shippingFixture(t, a, admin)

	// 當前倉庫庫存為 3 (shippingFixture: 1 + 2 = 3)
	// (A) 嘗試銷售出貨 10 件 (超過庫存 3) => 409
	soCreated := salesPost(t, a, admin, map[string]any{
		"action":     "create",
		"customerId": customer,
		"lines":      []map[string]any{{"itemId": item, "quantity": 10, "unitPrice": 500}},
	}, 200)
	soID := soCreated["id"].(string)
	salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": soID}, 200)

	var soLineID string
	if err := a.db.QueryRow("SELECT id FROM erp_sales_order_lines WHERE so_id=?", soID).Scan(&soLineID); err != nil {
		t.Fatal(err)
	}

	shipOverload := map[string]any{
		"action":      "ship",
		"soId":        soID,
		"warehouseId": warehouse,
		"lines":       []map[string]any{{"lineId": soLineID, "quantity": 10}},
		"requestId":   token(),
	}
	wShip := request(a, "POST", "/api/stations/erp/sales", shipOverload, admin...)
	if wShip.Code != 409 {
		t.Fatalf("sales shipment exceeding stock expected 409, got %d %s", wShip.Code, wShip.Body)
	}

	// 驗證庫存未被扣除 (依然為 3)
	var balance int64
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, warehouse).Scan(&balance); err != nil || balance != 3 {
		t.Fatalf("expected balance to remain 3, got %d (err: %v)", balance, err)
	}

	// (B) 嘗試調撥 10 件 (超過庫存 3) => 409
	destWarehouse := "dest_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Dest Warehouse',?)", destWarehouse, org, destWarehouse); err != nil {
		t.Fatal(err)
	}
	transferOverload := map[string]any{
		"action":          "transfer",
		"itemId":          item,
		"fromWarehouseId": warehouse,
		"toWarehouseId":   destWarehouse,
		"quantity":        10,
		"requestId":       token(),
	}
	wTransfer := request(a, "POST", "/api/stations/erp/inventory", transferOverload, admin...)
	if wTransfer.Code != 409 {
		t.Fatalf("stock transfer exceeding stock expected 409, got %d %s", wTransfer.Code, wTransfer.Body)
	}

	// 再次驗證來源倉與目的倉庫存
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, warehouse).Scan(&balance); err != nil || balance != 3 {
		t.Fatalf("expected balance after failed transfer to remain 3, got %d", balance)
	}
}

// 4. 調撥交易守恆：出庫倉庫減少數量必須完全等於入庫倉庫增加數量，且總量與價值守恆
func TestInventory_StockTransferConservation(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, _, item, source := shippingFixture(t, a, admin)

	dest := "warehouse_dest_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Destination',?)", dest, org, dest); err != nil {
		t.Fatal(err)
	}

	// 初始狀態：來源倉 3 件
	var sourceBefore, destBefore int64
	a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&sourceBefore)
	destBefore = 0

	// 執行調撥 2 件
	transferQty := int64(2)
	transferReq := map[string]any{
		"action":          "transfer",
		"itemId":          item,
		"fromWarehouseId": source,
		"toWarehouseId":   dest,
		"quantity":        transferQty,
		"requestId":       token(),
	}
	res := transferPost(t, a, admin, transferReq, 200)
	if res["transferId"] == nil {
		t.Fatal("missing transferId")
	}

	// 檢查調撥後各倉數量
	var sourceAfter, destAfter int64
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&sourceAfter); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, dest).Scan(&destAfter); err != nil {
		t.Fatal(err)
	}

	// 守恆性斷言：
	// 1. 來源倉減少量 == transferQty
	if sourceBefore-sourceAfter != transferQty {
		t.Fatalf("source warehouse decrement mismatch: expected %d, got %d", transferQty, sourceBefore-sourceAfter)
	}
	// 2. 目的倉增加量 == transferQty
	if destAfter-destBefore != transferQty {
		t.Fatalf("dest warehouse increment mismatch: expected %d, got %d", transferQty, destAfter-destBefore)
	}
	// 3. 總量守恆：sourceBefore + destBefore == sourceAfter + destAfter
	if (sourceBefore + destBefore) != (sourceAfter + destAfter) {
		t.Fatalf("total quantity not conserved: before=%d, after=%d", sourceBefore+destBefore, sourceAfter+destAfter)
	}

	// 4. 價值守恆：transfer_out total_value + transfer_in total_value == 0
	var sumValue int64
	refDoc := res["transferId"].(string)
	if err := a.db.QueryRow("SELECT sum(total_value) FROM erp_stock_ledger WHERE organization_id=? AND reference_doc=?", org, refDoc).Scan(&sumValue); err != nil || sumValue != 0 {
		t.Fatalf("transfer value not conserved: sum = %d (err: %v)", sumValue, err)
	}
}

// 5. 加權移動平均成本計算契約 (Moving Average Cost - E08)
// 契約驗證：10件@100 再 10件@200，均價150；出庫4件成本600、餘值2400
func TestInventory_MovingAverageCostE08(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org := journalOrg(t, a, admin)

	var supplier, customer string
	if err := a.db.QueryRow("SELECT id FROM erp_suppliers WHERE organization_id=? AND is_active=1 LIMIT 1", org).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow("SELECT id FROM erp_customers WHERE organization_id=? AND is_active=1 LIMIT 1", org).Scan(&customer); err != nil {
		t.Fatal(err)
	}

	item := "item_e08_" + token()
	warehouse := "wh_e08_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_items(id,organization_id,sku,name) VALUES(?,?,?,?)", item, org, item, "E08 Moving Avg Item"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,?,?)", warehouse, org, warehouse, warehouse); err != nil {
		t.Fatal(err)
	}

	// Step 1: 採購入庫 10件 @ 100
	po1 := newPurchase(t, a, admin, supplier, item)
	// 修改 po1 單價為 100，數量為 10
	if _, err := a.db.Exec("UPDATE erp_purchase_order_lines SET quantity=10, unit_price=100 WHERE po_id=?", po1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE erp_purchase_orders SET total_amount=1000 WHERE id=?", po1); err != nil {
		t.Fatal(err)
	}
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po1}, 200)

	var pol1 string
	if err := a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po1).Scan(&pol1); err != nil {
		t.Fatal(err)
	}
	purchasePost(t, a, admin, map[string]any{
		"action":      "receive",
		"poId":        po1,
		"warehouseId": warehouse,
		"lines":       []map[string]any{{"lineId": pol1, "quantity": 10}},
	}, 200)

	// 檢查第 1 次入庫後的均價
	var qty1, cost1, avg1 int64
	if err := a.db.QueryRow("SELECT total_quantity, total_cost, moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&qty1, &cost1, &avg1); err != nil {
		t.Fatal(err)
	}
	if qty1 != 10 || cost1 != 1000 || avg1 != 100 {
		t.Fatalf("Step 1 costing mismatch: qty=%d (want 10), cost=%d (want 1000), avg=%d (want 100)", qty1, cost1, avg1)
	}

	// Step 2: 採購入庫 10件 @ 200
	po2 := newPurchase(t, a, admin, supplier, item)
	if _, err := a.db.Exec("UPDATE erp_purchase_order_lines SET quantity=10, unit_price=200 WHERE po_id=?", po2); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE erp_purchase_orders SET total_amount=2000 WHERE id=?", po2); err != nil {
		t.Fatal(err)
	}
	purchasePost(t, a, admin, map[string]any{"action": "approve", "poId": po2}, 200)

	var pol2 string
	if err := a.db.QueryRow("SELECT id FROM erp_purchase_order_lines WHERE po_id=?", po2).Scan(&pol2); err != nil {
		t.Fatal(err)
	}
	purchasePost(t, a, admin, map[string]any{
		"action":      "receive",
		"poId":        po2,
		"warehouseId": warehouse,
		"lines":       []map[string]any{{"lineId": pol2, "quantity": 10}},
	}, 200)

	// 檢查第 2 次入庫後的均價：總量 20，總成本 3000，均價 150
	var qty2, cost2, avg2 int64
	if err := a.db.QueryRow("SELECT total_quantity, total_cost, moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&qty2, &cost2, &avg2); err != nil {
		t.Fatal(err)
	}
	if qty2 != 20 || cost2 != 3000 || avg2 != 150 {
		t.Fatalf("Step 2 costing mismatch: qty=%d (want 20), cost=%d (want 3000), avg=%d (want 150)", qty2, cost2, avg2)
	}

	// Step 3: 出庫 4件
	soCreated := salesPost(t, a, admin, map[string]any{
		"action":     "create",
		"customerId": customer,
		"lines":      []map[string]any{{"itemId": item, "quantity": 4, "unitPrice": 500}},
	}, 200)
	soID := soCreated["id"].(string)
	salesPost(t, a, admin, map[string]any{"action": "confirm", "soId": soID}, 200)

	var solID string
	if err := a.db.QueryRow("SELECT id FROM erp_sales_order_lines WHERE so_id=?", soID).Scan(&solID); err != nil {
		t.Fatal(err)
	}
	shipResult := salesPost(t, a, admin, map[string]any{
		"action":      "ship",
		"soId":        soID,
		"warehouseId": warehouse,
		"lines":       []map[string]any{{"lineId": solID, "quantity": 4}},
	}, 200)

	// 出庫成本結轉檢驗：出庫 4 件，成本應為 600
	extendedCost := int64(shipResult["extendedCost"].(float64))
	if extendedCost != 600 {
		t.Fatalf("COGS mismatch: expected 600, got %d", extendedCost)
	}

	// 剩餘庫存與剩餘總值檢驗：剩餘 16件，剩餘總成本 2400，均價維持 150
	var qty3, cost3, avg3 int64
	if err := a.db.QueryRow("SELECT total_quantity, total_cost, moving_average_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&qty3, &cost3, &avg3); err != nil {
		t.Fatal(err)
	}
	if qty3 != 16 || cost3 != 2400 || avg3 != 150 {
		t.Fatalf("Step 3 remaining costing mismatch: qty=%d (want 16), cost=%d (want 2400), avg=%d (want 150)", qty3, cost3, avg3)
	}
}

// 6. 租戶隔離：租戶 A 關帳不影響租戶 B，跨租戶資源嚴格隔離
func TestFiscalAndInventory_TenantIsolation(t *testing.T) {
	a := appForTest(t)
	spaceA := token()
	spaceB := token()
	adminA := login(t, a, "admin@suite.test", spaceA)
	adminB := login(t, a, "admin@suite.test", spaceB)

	orgA := journalOrg(t, a, adminA)
	orgB := journalOrg(t, a, adminB)
	if orgA == orgB {
		t.Fatal("tenants must differ")
	}

	// 租戶 A 關閉 2027-02
	periodA := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,1)",
		periodA, orgA, "P-A-2027-02", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}

	// 租戶 B 的 2027-02 處於開啟狀態
	periodB := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,0)",
		periodB, orgB, "P-B-2027-02", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}

	// 租戶 A 錄入 2027-02-15 傳票 => 被拒絕 409
	jeA := journalInput()
	jeA["entryDate"] = "2027-02-15"
	if w := request(a, "POST", "/api/stations/erp/journal", jeA, adminA...); w.Code != 409 {
		t.Fatalf("Org A journal in closed period expected 409, got %d", w.Code)
	}

	// 租戶 B 錄入 2027-02-15 傳票 => 成功 200
	jeB := journalInput()
	jeB["entryDate"] = "2027-02-15"
	if w := request(a, "POST", "/api/stations/erp/journal", jeB, adminB...); w.Code != 200 {
		t.Fatalf("Org B journal in open period expected 200, got %d %s", w.Code, w.Body)
	}

	// 租戶 A 嘗試關閉租戶 B 的會計期間 => 404 Not Found
	if w := request(a, "POST", "/api/stations/erp/fiscal-periods/"+periodB+"/close", nil, adminA...); w.Code != 404 {
		t.Fatalf("cross-tenant close period expected 404, got %d", w.Code)
	}
}

// 7. 冪等重試：相同金鑰與相同輸入回傳一致結果；相同金鑰修改輸入回傳 409
func TestIdempotencyRetry(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, _, item, source := shippingFixture(t, a, admin)
	dest := "warehouse_dest_" + token()
	if _, err := a.db.Exec("INSERT INTO erp_warehouses(id,organization_id,name,code) VALUES(?,?,'Idempotent Dest',?)", dest, org, dest); err != nil {
		t.Fatal(err)
	}

	reqID := token()
	transferPayload := map[string]any{
		"action":          "transfer",
		"itemId":          item,
		"fromWarehouseId": source,
		"toWarehouseId":   dest,
		"quantity":        1,
		"requestId":       reqID,
	}

	// 第一次請求
	first := transferPost(t, a, admin, transferPayload, 200)
	transferID := first["transferId"].(string)

	// 重試重放 (相同金鑰與負載) => 200，且 transferId 完全相同
	replay := transferPost(t, a, admin, transferPayload, 200)
	if replay["transferId"] != transferID {
		t.Fatalf("idempotent replay returned different transferId: %v vs %v", replay["transferId"], transferID)
	}

	// 驗證未重複扣減庫存 (來源倉原本3，調撥1，餘2；若重複扣會變1)
	var sourceBal int64
	if err := a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, source).Scan(&sourceBal); err != nil || sourceBal != 2 {
		t.Fatalf("expected source balance to remain 2 after replay, got %d", sourceBal)
	}

	// 篡改輸入再次請求相同金鑰 => 409 Conflict
	tamperedPayload := map[string]any{
		"action":          "transfer",
		"itemId":          item,
		"fromWarehouseId": source,
		"toWarehouseId":   dest,
		"quantity":        2,
		"requestId":       reqID,
	}
	wTampered := request(a, "POST", "/api/stations/erp/inventory", tamperedPayload, admin...)
	if wTampered.Code != 409 {
		t.Fatalf("tampered idempotency payload expected 409, got %d %s", wTampered.Code, wTampered.Body)
	}
}
