package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
)

type salesLine struct {
	ItemID    string `json:"itemId"`
	LineID    string `json:"lineId"`
	Quantity  int64  `json:"quantity"`
	UnitPrice int64  `json:"unitPrice"`
}
type salesInput struct {
	Action      string      `json:"action"`
	RequestID   string      `json:"requestId"`
	CustomerID  string      `json:"customerId"`
	SOID        string      `json:"soId"`
	WarehouseID string      `json:"warehouseId"`
	Lines       []salesLine `json:"lines"`
}

// Purchase and sales failures share the same internal HTTP error contract.
func (a *App) sales(w http.ResponseWriter, r *http.Request, id *Identity, grant *Grant) {
	if grant.Scope != "back" || (grant.Role != "operator" && grant.Role != "admin") {
		fail(w, 403, "Back-office sales permission required")
		return
	}
	var input salesInput
	if body(r, &input) != nil || len(input.RequestID) < 8 || len(input.RequestID) > 128 {
		fail(w, 400, "Provide a bounded sales request and request ID")
		return
	}
	if input.Action != "create" && input.Action != "confirm" && input.Action != "ship" {
		fail(w, 400, "Unknown sales operation")
		return
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		fail(w, 400, "Invalid sales input")
		return
	}
	hash, endpoint := digest(string(encoded)), "erp/sales/"+id.UserID
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Unable to begin sales transaction")
		return
	}
	defer tx.Rollback()
	var storedHash, cached string
	err = tx.QueryRow("SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", id.OrganizationID, input.RequestID, endpoint).Scan(&storedHash, &cached)
	if err == nil {
		if storedHash != hash {
			fail(w, 409, "Request ID already used for different input")
			return
		}
		var result map[string]any
		if json.Unmarshal([]byte(cached), &result) != nil {
			fail(w, 500, "Unable to read saved sales result")
			return
		}
		ok(w, result)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "Unable to verify sales replay")
		return
	}
	result, err := executeSales(tx, id, grant, input)
	if err != nil {
		var denied *purchaseError
		if errors.As(err, &denied) {
			fail(w, denied.status, denied.message)
		} else {
			fail(w, 500, "Sales transaction failed; no changes were saved")
		}
		return
	}
	encoded, err = json.Marshal(result)
	if err == nil {
		_, err = tx.Exec("INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)", token(), id.OrganizationID, input.RequestID, endpoint, hash, string(encoded))
	}
	if err != nil {
		fail(w, 500, "Unable to store sales replay result")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "Unable to commit sales transaction")
		return
	}
	ok(w, result)
}

func salesAudit(tx *sql.Tx, id *Identity, grant *Grant, action, order string, before, after any) error {
	b, err := json.Marshal(before)
	if err != nil {
		return err
	}
	c, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,before_value,after_value) VALUES(?,?,?,?,?,'ERP_SO',?,?,?)", token(), id.OrganizationID, id.UserID, grant.Role, action, order, string(b), string(c))
	return err
}

func roundedCost(cost, quantity int64) int64 {
	if quantity == 0 {
		return 0
	}
	result := cost / quantity
	if cost%quantity >= (quantity+1)/2 {
		result++
	}
	return result
}

func allocatedCost(cost, quantity, shipped int64) int64 {
	// The intermediate product can exceed int64 even though the result is bounded.
	value := new(big.Int).Mul(big.NewInt(cost), big.NewInt(shipped))
	value.Add(value, big.NewInt(quantity/2))
	return value.Quo(value, big.NewInt(quantity)).Int64()
}

func executeSales(tx *sql.Tx, id *Identity, grant *Grant, input salesInput) (map[string]any, error) {
	org := id.OrganizationID
	if input.Action == "create" {
		if len(input.Lines) < 1 || len(input.Lines) > 100 {
			return nil, purchaseFail(400, "Provide 1-100 sales lines")
		}
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM erp_customers WHERE id=? AND organization_id=? AND is_active=1", input.CustomerID, org).Scan(&count); err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, purchaseFail(400, "Active customer not found")
		}
		seen := map[string]bool{}
		var total int64
		for _, line := range input.Lines {
			if line.Quantity < 1 || line.Quantity > 10000000 || line.UnitPrice < 0 || line.UnitPrice > 10000000 || line.LineID != "" || seen[line.ItemID] {
				return nil, purchaseFail(400, "Invalid or duplicate sales item")
			}
			seen[line.ItemID] = true
			if err := tx.QueryRow("SELECT count(*) FROM erp_items WHERE id=? AND organization_id=? AND is_active=1", line.ItemID, org).Scan(&count); err != nil {
				return nil, err
			}
			if count != 1 {
				return nil, purchaseFail(400, "Active item not found")
			}
			amount := line.Quantity * line.UnitPrice
			if total > purchaseMax-amount {
				return nil, purchaseFail(400, "Sales total exceeds supported range")
			}
			total += amount
		}
		order, number := "so_"+token(), "SO-"+token()
		if _, err := tx.Exec("INSERT INTO erp_sales_orders(id,organization_id,so_number,customer_id,status,total_amount) VALUES(?,?,?,?,'draft',?)", order, org, number, input.CustomerID, total); err != nil {
			return nil, err
		}
		for _, line := range input.Lines {
			if _, err := tx.Exec("INSERT INTO erp_sales_order_lines(id,organization_id,so_id,item_id,quantity,unit_price) VALUES(?,?,?,?,?,?)", token(), org, order, line.ItemID, line.Quantity, line.UnitPrice); err != nil {
				return nil, err
			}
		}
		if err := salesAudit(tx, id, grant, "CREATE_SALES", order, nil, map[string]string{"status": "draft"}); err != nil {
			return nil, err
		}
		return map[string]any{"id": order, "soNumber": number, "status": "draft", "totalAmount": total}, nil
	}
	var status string
	if err := tx.QueryRow("SELECT status FROM erp_sales_orders WHERE organization_id=? AND id=?", org, input.SOID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, purchaseFail(404, "Sales order not found")
		}
		return nil, err
	}
	if input.Action == "confirm" {
		if status != "draft" {
			return nil, purchaseFail(409, "Only draft sales orders can be confirmed")
		}
		changed, err := tx.Exec("UPDATE erp_sales_orders SET status='confirmed' WHERE id=? AND organization_id=? AND status='draft'", input.SOID, org)
		if err != nil {
			return nil, err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, purchaseFail(409, "Sales status changed")
		}
		if err = salesAudit(tx, id, grant, "CONFIRM_SALES", input.SOID, map[string]string{"status": status}, map[string]string{"status": "confirmed"}); err != nil {
			return nil, err
		}
		return map[string]any{"id": input.SOID, "status": "confirmed"}, nil
	}
	if status != "confirmed" && status != "partially_shipped" {
		return nil, purchaseFail(409, "Only confirmed sales orders can ship")
	}
	if len(input.Lines) < 1 || len(input.Lines) > 100 {
		return nil, purchaseFail(400, "Provide bounded shipment lines")
	}
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM erp_warehouses WHERE id=? AND organization_id=?", input.WarehouseID, org).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, purchaseFail(400, "Warehouse not found")
	}
	shipment, number := "shipment_"+token(), "SH-"+token()
	if _, err := tx.Exec("INSERT INTO erp_shipments(id,organization_id,shipment_number,so_id,warehouse_id) VALUES(?,?,?,?,?)", shipment, org, number, input.SOID, input.WarehouseID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var totalCost int64
	for _, line := range input.Lines {
		if line.Quantity < 1 || line.Quantity > 10000000 || line.ItemID != "" || line.UnitPrice != 0 || seen[line.LineID] {
			return nil, purchaseFail(400, "Invalid or duplicate shipment line")
		}
		seen[line.LineID] = true
		var item string
		var ordered, shipped int64
		err := tx.QueryRow("SELECT item_id,quantity,shipped_quantity FROM erp_sales_order_lines WHERE id=? AND organization_id=? AND so_id=?", line.LineID, org, input.SOID).Scan(&item, &ordered, &shipped)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, purchaseFail(400, "Sales line not found")
			}
			return nil, err
		}
		if ordered < 0 || shipped < 0 || shipped > ordered || line.Quantity > ordered-shipped {
			return nil, purchaseFail(409, "Shipment exceeds remaining order quantity")
		}
		var stock int64
		err = tx.QueryRow("SELECT balance_after FROM erp_stock_ledger WHERE organization_id=? AND item_id=? AND warehouse_id=? ORDER BY rowid DESC LIMIT 1", org, item, input.WarehouseID).Scan(&stock)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if stock < line.Quantity || stock > purchaseMax {
			return nil, purchaseFail(409, "Warehouse stock is insufficient or invalid")
		}
		var costID string
		var quantity, cost int64
		err = tx.QueryRow("SELECT id,total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, item).Scan(&costID, &quantity, &cost)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, purchaseFail(409, "Inventory costing must be established before shipment")
			}
			return nil, err
		}
		if quantity < line.Quantity || quantity > purchaseMax || cost < 0 || cost > purchaseMax {
			return nil, purchaseFail(409, "Inventory costing is inconsistent")
		}
		allocated := allocatedCost(cost, quantity, line.Quantity)
		unit := roundedCost(allocated, line.Quantity)
		if totalCost > purchaseMax-allocated {
			return nil, purchaseFail(409, "Shipment cost exceeds supported range")
		}
		totalCost += allocated
		changed, err := tx.Exec("UPDATE erp_sales_order_lines SET shipped_quantity=shipped_quantity+? WHERE id=? AND organization_id=? AND so_id=? AND shipped_quantity=? AND quantity>=shipped_quantity+?", line.Quantity, line.LineID, org, input.SOID, shipped, line.Quantity)
		if err != nil {
			return nil, err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, purchaseFail(409, "Shipment quantity changed concurrently")
		}
		if _, err = tx.Exec("INSERT INTO erp_shipment_lines(id,organization_id,shipment_id,item_id,quantity,unit_cost,extended_cost) VALUES(?,?,?,?,?,?,?)", token(), org, shipment, item, line.Quantity, unit, allocated); err != nil {
			return nil, err
		}
		if _, err = tx.Exec("INSERT INTO erp_stock_ledger(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc) VALUES(?,?,?,?,'sales_shipment',?,?,?,?,?)", token(), org, item, input.WarehouseID, -line.Quantity, stock-line.Quantity, unit, -allocated, number); err != nil {
			return nil, err
		}
		remainingQty, remainingCost := quantity-line.Quantity, cost-allocated
		if _, err = tx.Exec("UPDATE erp_item_costing SET total_quantity=?,total_cost=?,moving_average_cost=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND organization_id=?", remainingQty, remainingCost, roundedCost(remainingCost, remainingQty), costID, org); err != nil {
			return nil, err
		}
	}
	var remaining int
	if err := tx.QueryRow("SELECT count(*) FROM erp_sales_order_lines WHERE organization_id=? AND so_id=? AND shipped_quantity<quantity", org, input.SOID).Scan(&remaining); err != nil {
		return nil, err
	}
	next := "partially_shipped"
	if remaining == 0 {
		next = "shipped"
	}
	if _, err := tx.Exec("UPDATE erp_sales_orders SET status=? WHERE id=? AND organization_id=?", next, input.SOID, org); err != nil {
		return nil, err
	}
	if err := salesAudit(tx, id, grant, "SHIP_SALES", input.SOID, map[string]string{"status": status}, map[string]any{"status": next, "shipmentId": shipment, "extendedCost": totalCost}); err != nil {
		return nil, err
	}
	return map[string]any{"id": shipment, "soId": input.SOID, "shipmentNumber": number, "status": next, "extendedCost": totalCost}, nil
}
