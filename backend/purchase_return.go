package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type purchaseReturnInput struct {
	ReceiptLineID string `json:"receiptLineId"`
	Quantity      int64  `json:"quantity"`
	Reason        string `json:"reason"`
	RequestID     string `json:"requestId"`
}

func purchaseReturnPermission(w http.ResponseWriter, g *Grant) bool {
	if g.Scope != "back" || (g.Role != "operator" && g.Role != "admin") {
		fail(w, 403, "Back-office purchase permission required")
		return false
	}
	return true
}

func (a *App) purchaseReturnAvailability(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if g.Scope != "back" && g.Scope != "front" {
		fail(w, 403, "Workspace purchase permission required")
		return
	}
	lineID := r.URL.Query().Get("receiptLineId")
	if lineID == "" {
		fail(w, 400, "Receipt line ID required")
		return
	}
	var itemID, warehouseID string
	var receivedQty int64
	err := a.db.QueryRow(`
		SELECT rl.item_id, rl.quantity, r.warehouse_id
		FROM erp_receipt_lines rl
		JOIN erp_receipts r ON r.id = rl.receipt_id AND r.organization_id = rl.organization_id
		WHERE rl.id = ? AND rl.organization_id = ?`, lineID, id.OrganizationID).Scan(&itemID, &receivedQty, &warehouseID)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Receipt line not found")
		return
	}
	if err != nil {
		fail(w, 503, "Unable to read receipt line")
		return
	}
	var priorReturned int64
	err = a.db.QueryRow("SELECT coalesce(-sum(quantity_change), 0) FROM erp_stock_ledger WHERE organization_id=? AND change_type='purchase_return' AND reference_doc=?", id.OrganizationID, lineID).Scan(&priorReturned)
	if err != nil {
		fail(w, 503, "Unable to check prior returns")
		return
	}
	remainingLine := receivedQty - priorReturned
	if remainingLine < 0 {
		remainingLine = 0
	}
	var warehouseStock int64
	err = a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", id.OrganizationID, itemID, warehouseID).Scan(&warehouseStock)
	if errors.Is(err, sql.ErrNoRows) {
		warehouseStock = 0
	} else if err != nil {
		fail(w, 503, "Unable to check warehouse stock")
		return
	}
	available := remainingLine
	if warehouseStock < available {
		available = warehouseStock
	}
	if available < 0 {
		available = 0
	}
	ok(w, map[string]any{
		"receiptLineId":     lineID,
		"itemId":            itemID,
		"warehouseId":       warehouseID,
		"receivedQuantity":  receivedQty,
		"returnedQuantity":  priorReturned,
		"remainingQuantity": remainingLine,
		"warehouseStock":    warehouseStock,
		"available":         available,
	})
}

func (a *App) purchaseReturn(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if !purchaseReturnPermission(w, g) {
		return
	}
	var in purchaseReturnInput
	if body(r, &in) != nil || len(in.RequestID) < 8 || len(in.RequestID) > 128 || in.ReceiptLineID == "" || in.Quantity < 1 || in.Quantity > purchaseMax {
		fail(w, 400, "Provide a valid receipt line, positive integer quantity, reason and request ID")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || len(in.Reason) > 500 {
		fail(w, 400, "Provide a valid return reason")
		return
	}
	payload, err := json.Marshal(in)
	if err != nil {
		fail(w, 400, "Invalid return input")
		return
	}
	hash, endpoint := digest(string(payload)), "erp/purchase-returns/"+id.UserID
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 503, "Unable to begin purchase return")
		return
	}
	defer tx.Rollback()

	var prior, cached string
	err = tx.QueryRow("SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", id.OrganizationID, in.RequestID, endpoint).Scan(&prior, &cached)
	if err == nil {
		if prior != hash {
			fail(w, 409, "Request ID already used for different input")
			return
		}
		var result map[string]any
		if json.Unmarshal([]byte(cached), &result) != nil {
			fail(w, 503, "Unable to read saved return result")
			return
		}
		ok(w, result)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 503, "Unable to verify return retry")
		return
	}

	org := id.OrganizationID
	var itemID, warehouseID string
	var receivedQty int64
	err = tx.QueryRow(`
		SELECT rl.item_id, rl.quantity, r.warehouse_id
		FROM erp_receipt_lines rl
		JOIN erp_receipts r ON r.id = rl.receipt_id AND r.organization_id = rl.organization_id
		WHERE rl.id = ? AND rl.organization_id = ?`, in.ReceiptLineID, org).Scan(&itemID, &receivedQty, &warehouseID)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Receipt line not found")
		return
	}
	if err != nil {
		fail(w, 503, "Unable to validate receipt line")
		return
	}

	var priorReturned int64
	err = tx.QueryRow("SELECT coalesce(-sum(quantity_change), 0) FROM erp_stock_ledger WHERE organization_id=? AND change_type='purchase_return' AND reference_doc=?", org, in.ReceiptLineID).Scan(&priorReturned)
	if err != nil {
		fail(w, 503, "Unable to check prior returns")
		return
	}
	remainingLine := receivedQty - priorReturned
	if in.Quantity > remainingLine {
		fail(w, 409, "Return quantity exceeds remaining receipt quantity")
		return
	}

	var warehouseStock int64
	err = tx.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, itemID, warehouseID).Scan(&warehouseStock)
	if errors.Is(err, sql.ErrNoRows) {
		warehouseStock = 0
	} else if err != nil {
		fail(w, 503, "Unable to check warehouse stock")
		return
	}
	if warehouseStock < in.Quantity {
		fail(w, 409, "Warehouse stock is insufficient for return")
		return
	}

	var costID string
	var totalQty, totalCost int64
	err = tx.QueryRow("SELECT id,total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, itemID).Scan(&costID, &totalQty, &totalCost)
	if err != nil {
		fail(w, 409, "Inventory costing must exist")
		return
	}
	if totalQty < in.Quantity || totalQty > purchaseMax || totalCost < 0 || totalCost > purchaseMax {
		fail(w, 409, "Inventory costing is inconsistent")
		return
	}
	var globalQty int64
	err = tx.QueryRow("SELECT coalesce(sum(balance_after),0) FROM (SELECT balance_after,row_number() OVER (PARTITION BY warehouse_id ORDER BY rowid DESC) rn FROM erp_stock_ledger WHERE organization_id=? AND item_id=?) WHERE rn=1", org, itemID).Scan(&globalQty)
	if err != nil || globalQty < 0 || globalQty > purchaseMax || globalQty != totalQty {
		fail(w, 409, "Global stock quantity and item costing are inconsistent")
		return
	}

	allocated := allocatedCost(totalCost, totalQty, in.Quantity)
	unit := roundedCost(allocated, in.Quantity)

	newBalance := warehouseStock - in.Quantity
	if _, err = tx.Exec("INSERT INTO erp_stock_ledger(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc) VALUES(?,?,?,?,'purchase_return',?,?,?,?,?)", token(), org, itemID, warehouseID, -in.Quantity, newBalance, unit, -allocated, in.ReceiptLineID); err != nil {
		fail(w, 503, "Unable to update stock ledger")
		return
	}

	remainingQty := totalQty - in.Quantity
	remainingCost := totalCost - allocated
	if _, err = tx.Exec("UPDATE erp_item_costing SET total_quantity=?,total_cost=?,moving_average_cost=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND organization_id=?", remainingQty, remainingCost, roundedCost(remainingCost, remainingQty), costID, org); err != nil {
		fail(w, 503, "Unable to update item costing")
		return
	}

	returnID := token()
	if _, err = tx.Exec("INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,reason,request_id) VALUES(?,?,?,?,'erp.purchase_return','receipt_line',?,?,?)", returnID, org, id.UserID, g.Role, in.ReceiptLineID, in.Reason, in.RequestID); err != nil {
		fail(w, 503, "Unable to record return audit")
		return
	}

	result := map[string]any{
		"id":            returnID,
		"receiptLineId": in.ReceiptLineID,
		"quantity":      in.Quantity,
		"unitCost":      unit,
		"totalCost":     allocated,
		"reason":        in.Reason,
		"status":        "completed",
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		fail(w, 503, "Unable to format return response")
		return
	}
	if _, err = tx.Exec("INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)", token(), org, in.RequestID, endpoint, hash, string(resultJSON)); err != nil {
		fail(w, 503, "Unable to save return retry key")
		return
	}

	if err = tx.Commit(); err != nil {
		fail(w, 503, "Unable to commit purchase return")
		return
	}
	ok(w, result)
}
