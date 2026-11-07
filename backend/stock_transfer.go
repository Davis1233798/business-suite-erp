package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

type transferInput struct {
	Action string `json:"action"`
	ItemID string `json:"itemId"`
	FromWarehouseID string `json:"fromWarehouseId"`
	ToWarehouseID string `json:"toWarehouseId"`
	Quantity int64 `json:"quantity"`
	RequestID string `json:"requestId"`
}

func transferPermission(w http.ResponseWriter, g *Grant) bool {
	if g.Scope != "back" || (g.Role != "operator" && g.Role != "admin") {
		fail(w, 403, "Back-office inventory permission required")
		return false
	}
	return true
}

func (a *App) inventoryAvailability(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if g.Scope != "back" && g.Scope != "front" { fail(w, 403, "Workspace inventory permission required"); return }
	item, source := r.URL.Query().Get("itemId"), r.URL.Query().Get("warehouseId")
	if item == "" || source == "" { fail(w, 400, "Select an item and warehouse"); return }
	var active int
	err := a.db.QueryRow("SELECT count(*) FROM erp_items WHERE id=? AND organization_id=? AND is_active=1", item, id.OrganizationID).Scan(&active)
	if err != nil { fail(w, 503, "Unable to read stock availability"); return }
	if active != 1 { fail(w, 404, "Active item not found"); return }
	var warehouse int
	err = a.db.QueryRow("SELECT count(*) FROM erp_warehouses WHERE id=? AND organization_id=?", source, id.OrganizationID).Scan(&warehouse)
	if err != nil { fail(w, 503, "Unable to read stock availability"); return }
	if warehouse != 1 { fail(w, 404, "Warehouse not found"); return }
	var balance int64
	err = a.db.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", id.OrganizationID, item, source).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) { balance = 0 } else if err != nil { fail(w, 503, "Unable to read stock availability"); return }
	if balance < 0 || balance > purchaseMax { fail(w, 409, "Stock balance is invalid"); return }
	ok(w, map[string]any{"itemId": item, "warehouseId": source, "available": balance})
}

func (a *App) transferInventory(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if !transferPermission(w, g) { return }
	var in transferInput
	if body(r, &in) != nil || in.Action != "transfer" || len(in.RequestID) < 8 || len(in.RequestID) > 128 || in.ItemID == "" || in.FromWarehouseID == "" || in.ToWarehouseID == "" || in.Quantity < 1 || in.Quantity > purchaseMax {
		fail(w, 400, "Provide a valid transfer, positive integer quantity and request ID"); return
	}
	if in.FromWarehouseID == in.ToWarehouseID { fail(w, 400, "Source and destination warehouses must differ"); return }
	payload, err := json.Marshal(in); if err != nil { fail(w, 400, "Invalid transfer input"); return }
	hash, endpoint := digest(string(payload)), "erp/inventory/"+id.UserID
	tx, err := a.db.BeginTx(r.Context(), nil); if err != nil { fail(w, 503, "Unable to begin stock transfer"); return }; defer tx.Rollback()
	var prior, cached string
	err = tx.QueryRow("SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", id.OrganizationID, in.RequestID, endpoint).Scan(&prior, &cached)
	if err == nil {
		if prior != hash { fail(w, 409, "Request ID already used for different input"); return }
		var result map[string]any; if json.Unmarshal([]byte(cached), &result) != nil { fail(w, 503, "Unable to read saved transfer result"); return }; ok(w, result); return
	}
	if !errors.Is(err, sql.ErrNoRows) { fail(w, 503, "Unable to verify transfer retry"); return }
	org := id.OrganizationID
	var active int
	if err = tx.QueryRow("SELECT count(*) FROM erp_items WHERE id=? AND organization_id=? AND is_active=1", in.ItemID, org).Scan(&active); err != nil { fail(w, 503, "Unable to validate transfer item"); return }
	if active != 1 { fail(w, 404, "Active item not found"); return }
	for _, warehouse := range []string{in.FromWarehouseID, in.ToWarehouseID} {
		if err = tx.QueryRow("SELECT count(*) FROM erp_warehouses WHERE id=? AND organization_id=?", warehouse, org).Scan(&active); err != nil { fail(w, 503, "Unable to validate transfer warehouse"); return }
		if active != 1 { fail(w, 404, "Warehouse not found in this organization"); return }
	}
	var source, dest int64
	err = tx.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, in.ItemID, in.FromWarehouseID).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) { source = 0 } else if err != nil { fail(w, 503, "Unable to read source stock"); return }
	err = tx.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, in.ItemID, in.ToWarehouseID).Scan(&dest)
	if errors.Is(err, sql.ErrNoRows) { dest = 0 } else if err != nil { fail(w, 503, "Unable to read destination stock"); return }
	if source < in.Quantity { fail(w, 409, "Source warehouse stock is insufficient"); return }
	if source > purchaseMax || dest < 0 || dest > purchaseMax-in.Quantity { fail(w, 409, "Warehouse stock exceeds supported range"); return }
	var costID string; var totalQty, totalCost int64
	err = tx.QueryRow("SELECT id,total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, in.ItemID).Scan(&costID, &totalQty, &totalCost)
	if err != nil { fail(w, 409, "Inventory costing must be established before transfer"); return }
	if totalQty < in.Quantity || totalQty < 1 || totalQty > purchaseMax || totalCost < 0 || totalCost > purchaseMax { fail(w, 409, "Inventory costing is inconsistent"); return }
	var globalQty int64
	err = tx.QueryRow("SELECT coalesce(sum(balance_after),0) FROM (SELECT balance_after,row_number() OVER (PARTITION BY warehouse_id ORDER BY rowid DESC) rn FROM erp_stock_ledger WHERE organization_id=? AND item_id=?) WHERE rn=1", org, in.ItemID).Scan(&globalQty)
	if err != nil || globalQty < 0 || globalQty > purchaseMax || globalQty != totalQty { fail(w, 409, "Global stock quantity and item costing are inconsistent"); return }
	movedCost := allocatedCost(totalCost, totalQty, in.Quantity)
	unit := roundedCost(movedCost, in.Quantity)
	ref := "TR-"+token()
	if _, err = tx.Exec("INSERT INTO erp_stock_ledger(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc) VALUES(?,?,?,?,'transfer_out',?,?,?,?,?)", token(),org,in.ItemID,in.FromWarehouseID,-in.Quantity,source-in.Quantity,unit,-movedCost,ref); err != nil { fail(w, 503, "Transfer failed; no changes were saved"); return }
	if _, err = tx.Exec("INSERT INTO erp_stock_ledger(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc) VALUES(?,?,?,?,'transfer_in',?,?,?,?,?)", token(),org,in.ItemID,in.ToWarehouseID,in.Quantity,dest+in.Quantity,unit,movedCost,ref); err != nil { fail(w, 503, "Transfer failed; no changes were saved"); return }
	result := map[string]any{"transferId": ref, "sourceBalanceAfter": source-in.Quantity, "destBalanceAfter": dest+in.Quantity, "quantity": in.Quantity, "totalValue": movedCost}
	before, _ := json.Marshal(map[string]any{"sourceBalance":source,"destinationBalance":dest,"totalQuantity":totalQty,"totalCost":totalCost})
	after, _ := json.Marshal(map[string]any{"sourceBalance":source-in.Quantity,"destinationBalance":dest+in.Quantity,"totalQuantity":totalQty,"totalCost":totalCost,"transferId":ref,"quantity":in.Quantity,"totalValue":movedCost})
	if _, err = tx.Exec("INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,before_value,after_value,request_id) VALUES(?,?,?,?,'TRANSFER_STOCK','ERP_STOCK_TRANSFER',?,?,?,?)",token(),org,id.UserID,g.Role,ref,string(before),string(after),in.RequestID); err != nil { fail(w,503,"Transfer audit failed; no changes were saved"); return }
	encoded, _ := json.Marshal(result)
	if _, err = tx.Exec("INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)",token(),org,in.RequestID,endpoint,hash,string(encoded)); err != nil { fail(w,503,"Unable to save transfer retry result"); return }
	if err = tx.Commit(); err != nil { fail(w,503,"Unable to commit stock transfer"); return }
	ok(w,result)
}
