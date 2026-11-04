package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const purchaseMax int64 = 9007199254740991

type purchaseLine struct {
	ItemID    string `json:"itemId"`
	LineID    string `json:"lineId"`
	Quantity  int64  `json:"quantity"`
	UnitPrice int64  `json:"unitPrice"`
	IsBoxes   bool   `json:"isBoxes"`
}
type purchaseInput struct {
	Action      string         `json:"action"`
	RequestID   string         `json:"requestId"`
	SupplierID  string         `json:"supplierId"`
	POID        string         `json:"poId"`
	WarehouseID string         `json:"warehouseId"`
	Reason      string         `json:"reason"`
	Lines       []purchaseLine `json:"lines"`
}
type purchaseError struct {
	status  int
	message string
}

func (e *purchaseError) Error() string              { return e.message }
func purchaseFail(status int, message string) error { return &purchaseError{status, message} }
func (a *App) purchases(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if g.Scope != "back" || (g.Role != "operator" && g.Role != "admin") {
		fail(w, 403, "Back-office purchase permission required")
		return
	}
	var p purchaseInput
	if body(r, &p) != nil {
		fail(w, 400, "Invalid purchase request")
		return
	}
	p.Reason = strings.TrimSpace(p.Reason)
	if len(p.RequestID) < 8 || len(p.RequestID) > 128 {
		fail(w, 400, "A bounded request ID is required")
		return
	}
	if p.Action != "create" && p.Action != "approve" && p.Action != "reject" && p.Action != "receive" {
		fail(w, 400, "Unknown purchase operation")
		return
	}
	if (p.Action == "approve" || p.Action == "reject") && g.Role != "admin" {
		fail(w, 403, "Only administrators can decide purchase orders")
		return
	}
	payload, e := json.Marshal(p)
	if e != nil {
		fail(w, 400, "Invalid purchase input")
		return
	}
	hash := digest(string(payload))
	endpoint := "erp/purchases/" + id.UserID
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Unable to begin purchase transaction")
		return
	}
	defer tx.Rollback()
	var oldHash, cached string
	e = tx.QueryRow("SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", id.OrganizationID, p.RequestID, endpoint).Scan(&oldHash, &cached)
	if e == nil {
		if oldHash != hash {
			fail(w, 409, "Request ID already used for different input")
			return
		}
		var result map[string]any
		if json.Unmarshal([]byte(cached), &result) != nil {
			fail(w, 500, "Unable to read stored purchase result")
			return
		}
		ok(w, result)
		return
	}
	if !errors.Is(e, sql.ErrNoRows) {
		fail(w, 500, "Unable to verify purchase replay")
		return
	}
	result, e := executePurchase(tx, id, g, p)
	if e != nil {
		var denied *purchaseError
		if errors.As(e, &denied) {
			fail(w, denied.status, denied.message)
		} else {
			fail(w, 500, "Purchase transaction failed; no changes were saved")
		}
		return
	}
	encoded, e := json.Marshal(result)
	if e == nil {
		_, e = tx.Exec("INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)", token(), id.OrganizationID, p.RequestID, endpoint, hash, string(encoded))
	}
	if e != nil {
		fail(w, 500, "Unable to save purchase replay result")
		return
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "Unable to commit purchase transaction")
		return
	}
	ok(w, result)
}
func purchaseAudit(tx *sql.Tx, id *Identity, g *Grant, action, po string, before, after any, reason string) error {
	beforeJSON, e := json.Marshal(before)
	if e != nil {
		return e
	}
	afterJSON, e := json.Marshal(after)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,before_value,after_value,reason) VALUES(?,?,?,?,?,'ERP_PO',?,?,?,?)", token(), id.OrganizationID, id.UserID, g.Role, action, po, string(beforeJSON), string(afterJSON), reason)
	return e
}
func executePurchase(tx *sql.Tx, id *Identity, g *Grant, p purchaseInput) (map[string]any, error) {
	org := id.OrganizationID
	if p.Action == "create" {
		if len(p.Lines) < 1 || len(p.Lines) > 100 {
			return nil, purchaseFail(400, "Provide 1-100 distinct purchase items")
		}
		var exists int
		if e := tx.QueryRow("SELECT count(*) FROM erp_suppliers WHERE id=? AND organization_id=? AND is_active=1", p.SupplierID, org).Scan(&exists); e != nil {
			return nil, e
		}
		if exists != 1 {
			return nil, purchaseFail(400, "Active supplier not found")
		}
		seen := map[string]bool{}
		var total int64
		for _, l := range p.Lines {
			if l.Quantity < 1 || l.Quantity > 10000000 || l.UnitPrice < 0 || l.UnitPrice > 10000000 || seen[l.ItemID] || l.LineID != "" || l.IsBoxes {
				return nil, purchaseFail(400, "Invalid or duplicate purchase line")
			}
			seen[l.ItemID] = true
			if e := tx.QueryRow("SELECT count(*) FROM erp_items WHERE id=? AND organization_id=? AND is_active=1", l.ItemID, org).Scan(&exists); e != nil {
				return nil, e
			}
			if exists != 1 {
				return nil, purchaseFail(400, "Active item not found")
			}
			amount := l.Quantity * l.UnitPrice
			if total > purchaseMax-amount {
				return nil, purchaseFail(400, "Purchase amount exceeds supported range")
			}
			total += amount
		}
		po := "po_" + token()
		number := "PO-" + token()
		if _, e := tx.Exec("INSERT INTO erp_purchase_orders(id,organization_id,po_number,supplier_id,status,total_amount) VALUES(?,?,?,?,'draft',?)", po, org, number, p.SupplierID, total); e != nil {
			return nil, e
		}
		for _, l := range p.Lines {
			if _, e := tx.Exec("INSERT INTO erp_purchase_order_lines(id,organization_id,po_id,item_id,quantity,unit_price) VALUES(?,?,?,?,?,?)", "pol_"+token(), org, po, l.ItemID, l.Quantity, l.UnitPrice); e != nil {
				return nil, e
			}
		}
		if e := purchaseAudit(tx, id, g, "CREATE_PO", po, nil, map[string]string{"status": "draft"}, ""); e != nil {
			return nil, e
		}
		return map[string]any{"id": po, "poNumber": number, "status": "draft", "totalAmount": total}, nil
	}
	var status string
	if e := tx.QueryRow("SELECT status FROM erp_purchase_orders WHERE organization_id=? AND id=?", org, p.POID).Scan(&status); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, purchaseFail(404, "Purchase order not found")
		}
		return nil, e
	}
	if p.Action == "approve" || p.Action == "reject" {
		if status != "draft" {
			return nil, purchaseFail(409, "Only draft orders can be approved or rejected")
		}
		next := "approved"
		if p.Action == "reject" {
			next = "rejected"
			if p.Reason == "" || len(p.Reason) > 500 {
				return nil, purchaseFail(400, "Rejection needs a bounded reason")
			}
		}
		changed, e := tx.Exec("UPDATE erp_purchase_orders SET status=?,rejected_reason=? WHERE id=? AND organization_id=? AND status='draft'", next, p.Reason, p.POID, org)
		if e != nil {
			return nil, e
		}
		n, e := changed.RowsAffected()
		if e != nil {
			return nil, e
		}
		if n != 1 {
			return nil, purchaseFail(409, "Purchase state changed")
		}
		if e = purchaseAudit(tx, id, g, "DECIDE_PO", p.POID, map[string]string{"status": status}, map[string]string{"status": next}, p.Reason); e != nil {
			return nil, e
		}
		return map[string]any{"id": p.POID, "status": next}, nil
	}
	if status != "approved" {
		return nil, purchaseFail(409, "Only approved orders can receive goods")
	}
	if len(p.Lines) < 1 || len(p.Lines) > 100 {
		return nil, purchaseFail(400, "Provide bounded receipt lines")
	}
	var exists int
	if e := tx.QueryRow("SELECT count(*) FROM erp_warehouses WHERE id=? AND organization_id=?", p.WarehouseID, org).Scan(&exists); e != nil {
		return nil, e
	}
	if exists != 1 {
		return nil, purchaseFail(400, "Warehouse not found")
	}
	receipt := "receipt_" + token()
	number := "RC-" + token()
	if _, e := tx.Exec("INSERT INTO erp_receipts(id,organization_id,receipt_number,po_id,warehouse_id) VALUES(?,?,?,?,?)", receipt, org, number, p.POID, p.WarehouseID); e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for _, l := range p.Lines {
		if l.Quantity < 1 || l.Quantity > 10000000 || l.ItemID != "" || l.UnitPrice != 0 || seen[l.LineID] {
			return nil, purchaseFail(400, "Invalid or duplicate receipt line")
		}
		seen[l.LineID] = true
		var item string
		var ordered, received, price, ratio int64
		e := tx.QueryRow("SELECT l.item_id,l.quantity,l.received_quantity,l.unit_price,i.box_conversion_ratio FROM erp_purchase_order_lines l JOIN erp_items i ON i.id=l.item_id AND i.organization_id=l.organization_id WHERE l.id=? AND l.organization_id=? AND l.po_id=?", l.LineID, org, p.POID).Scan(&item, &ordered, &received, &price, &ratio)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, purchaseFail(400, "Purchase line not found")
			}
			return nil, e
		}
		pieces := l.Quantity
		if l.IsBoxes {
			if ratio < 1 || ratio > 10000000 || pieces > 10000000/ratio {
				return nil, purchaseFail(400, "Invalid box quantity")
			}
			pieces *= ratio
		}
		if pieces > ordered-received {
			return nil, purchaseFail(409, "Receipt exceeds remaining quantity")
		}
		if price < 0 || price > 10000000 {
			return nil, purchaseFail(409, "Stored item cost is outside supported range")
		}
		changed, e := tx.Exec("UPDATE erp_purchase_order_lines SET received_quantity=received_quantity+? WHERE id=? AND organization_id=? AND po_id=? AND received_quantity=? AND quantity>=received_quantity+?", pieces, l.LineID, org, p.POID, received, pieces)
		if e != nil {
			return nil, e
		}
		n, e := changed.RowsAffected()
		if e != nil {
			return nil, e
		}
		if n != 1 {
			return nil, purchaseFail(409, "Receipt quantity changed concurrently")
		}
		if _, e = tx.Exec("INSERT INTO erp_receipt_lines(id,organization_id,receipt_id,item_id,quantity,unit_cost) VALUES(?,?,?,?,?,?)", token(), org, receipt, item, pieces, price); e != nil {
			return nil, e
		}
		var balance int64
		e = tx.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, p.WarehouseID).Scan(&balance)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if balance < 0 || balance > purchaseMax-pieces {
			return nil, purchaseFail(409, "Stock balance exceeds supported range")
		}
		if _, e = tx.Exec("INSERT INTO erp_stock_ledger(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc) VALUES(?,?,?,?,'purchase_receipt',?,?,?,?,?)", token(), org, item, p.WarehouseID, pieces, balance+pieces, price, pieces*price, number); e != nil {
			return nil, e
		}
		var costID string
		var qty, cost int64
		e = tx.QueryRow("SELECT id,total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&costID, &qty, &cost)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		incoming := pieces * price
		if qty < 0 || cost < 0 || qty > purchaseMax-pieces || cost > purchaseMax-incoming {
			return nil, purchaseFail(409, "Item costing exceeds supported range")
		}
		qty += pieces
		cost += incoming
		average := cost / qty
		if cost%qty >= (qty+1)/2 {
			average++
		}
		if costID == "" {
			_, e = tx.Exec("INSERT INTO erp_item_costing(id,organization_id,item_id,total_quantity,total_cost,moving_average_cost) VALUES(?,?,?,?,?,?)", token(), org, item, qty, cost, average)
		} else {
			_, e = tx.Exec("UPDATE erp_item_costing SET total_quantity=?,total_cost=?,moving_average_cost=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND organization_id=?", qty, cost, average, costID, org)
		}
		if e != nil {
			return nil, e
		}
	}
	var remaining int
	if e := tx.QueryRow("SELECT count(*) FROM erp_purchase_order_lines WHERE organization_id=? AND po_id=? AND received_quantity<quantity", org, p.POID).Scan(&remaining); e != nil {
		return nil, e
	}
	next := "approved"
	if remaining == 0 {
		next = "received"
	}
	if _, e := tx.Exec("UPDATE erp_purchase_orders SET status=? WHERE id=? AND organization_id=?", next, p.POID, org); e != nil {
		return nil, e
	}
	if e := purchaseAudit(tx, id, g, "RECEIVE_PO", p.POID, map[string]string{"status": status}, map[string]string{"status": next, "receiptId": receipt}, ""); e != nil {
		return nil, e
	}
	return map[string]any{"id": receipt, "receiptNumber": number, "poId": p.POID, "status": next}, nil
}
