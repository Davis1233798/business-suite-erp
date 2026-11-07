package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
)

type countInput struct {
	ItemID      string `json:"itemId"`
	WarehouseID string `json:"warehouseId"`
	Expected    *int64 `json:"expectedQuantity"`
	Version     string `json:"expectedVersion"`
	Counted     *int64 `json:"countedQuantity"`
	Reason      string `json:"reason"`
	Key         string `json:"idempotencyKey"`
}

type countSnapshot struct {
	Quantity int64  `json:"quantity"`
	Version  string `json:"version"`
}

type countQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readCountSnapshot(ctx context.Context, query countQuery, org, item, warehouse string) (countSnapshot, error) {
	var current countSnapshot
	err := query.QueryRowContext(ctx, `
		SELECT COALESCE(l.balance_after, 0), COALESCE(l.id, '')
		FROM erp_items i
		JOIN erp_warehouses w ON w.organization_id=i.organization_id AND w.id=? AND w.is_active=1
		LEFT JOIN erp_stock_ledger l ON l.id=(
			SELECT id FROM erp_stock_ledger
			WHERE organization_id=i.organization_id AND item_id=i.id AND warehouse_id=w.id
			ORDER BY rowid DESC LIMIT 1
		)
		WHERE i.organization_id=? AND i.id=? AND i.is_active=1
	`, warehouse, org, item).Scan(&current.Quantity, &current.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return current, purchaseFail(404, "Active item or warehouse not found")
	}
	if err == nil && (current.Quantity < 0 || current.Quantity > purchaseMax) {
		return current, purchaseFail(409, "Warehouse balance is inconsistent")
	}
	return current, err
}

func countFailure(w http.ResponseWriter, err error) {
	var known *purchaseError
	if errors.As(err, &known) {
		fail(w, known.status, known.message)
		return
	}
	fail(w, 503, "Unable to record the stock count")
}

func (a *App) stockCount(w http.ResponseWriter, r *http.Request, id *Identity, grant *Grant) {
	if grant.Role != "admin" || grant.Scope != "back" {
		fail(w, 403, "Back-office administrator access required")
		return
	}
	ctx, org := r.Context(), id.OrganizationID
	switch r.Method {
	case http.MethodGet:
		item, warehouse := r.URL.Query().Get("itemId"), r.URL.Query().Get("warehouseId")
		if item == "" || warehouse == "" || len(item) > 256 || len(warehouse) > 256 {
			fail(w, 400, "Select an item and warehouse")
			return
		}
		current, err := readCountSnapshot(ctx, a.db, org, item, warehouse)
		if err != nil {
			countFailure(w, err)
			return
		}
		ok(w, current)
	case http.MethodPost:
		var in countInput
		if body(r, &in) != nil {
			fail(w, 400, "Invalid stock count")
			return
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.ItemID == "" || len(in.ItemID) > 256 || in.WarehouseID == "" || len(in.WarehouseID) > 256 ||
			len(in.Version) > 256 || in.Expected == nil || *in.Expected < 0 || *in.Expected > purchaseMax ||
			in.Counted == nil || *in.Counted < 0 || *in.Counted > purchaseMax ||
			in.Reason == "" || len(in.Reason) > 500 || len(in.Key) < 8 || len(in.Key) > 128 {
			fail(w, 400, "Provide the current stock snapshot, counted quantity and reason")
			return
		}
		result, err := a.recordStockCount(ctx, id, grant, in)
		if err != nil {
			countFailure(w, err)
			return
		}
		ok(w, result)
	default:
		fail(w, 405, "Use GET or POST")
	}
}

func (a *App) recordStockCount(ctx context.Context, id *Identity, grant *Grant, in countInput) (json.RawMessage, error) {
	org := id.OrganizationID
	payload, _ := json.Marshal(in)
	hash, endpoint := digest(string(payload)), "erp/stock-count/"+id.UserID
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var prior, response string
	err = tx.QueryRowContext(ctx, "SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", org, in.Key, endpoint).Scan(&prior, &response)
	if err == nil {
		if prior != hash {
			return nil, purchaseFail(409, "Retry key already used for a different count")
		}
		if !json.Valid([]byte(response)) {
			return nil, errors.New("invalid stored stock count")
		}
		return json.RawMessage(response), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	current, err := readCountSnapshot(ctx, tx, org, in.ItemID, in.WarehouseID)
	if err != nil {
		return nil, err
	}
	if current.Quantity != *in.Expected || current.Version != in.Version {
		return nil, purchaseFail(409, "Inventory changed. Refresh the count before submitting")
	}
	delta := *in.Counted - current.Quantity
	if delta == 0 {
		return nil, purchaseFail(409, "Count matches current inventory")
	}

	var costID string
	var quantity, cost int64
	err = tx.QueryRowContext(ctx, "SELECT id,total_quantity,total_cost FROM erp_item_costing WHERE organization_id=? AND item_id=?", org, in.ItemID).Scan(&costID, &quantity, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, purchaseFail(409, "The item has no established inventory cost")
	}
	if err != nil {
		return nil, err
	}
	var ledgerQuantity, minimum int64
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(balance_after),0), COALESCE(MIN(balance_after),0)
		FROM (SELECT balance_after, ROW_NUMBER() OVER (PARTITION BY warehouse_id ORDER BY rowid DESC) AS rank
			FROM erp_stock_ledger WHERE organization_id=? AND item_id=?)
		WHERE rank=1
	`, org, in.ItemID).Scan(&ledgerQuantity, &minimum)
	if err != nil {
		return nil, err
	}
	if quantity < 1 || quantity > purchaseMax || cost < 0 || cost > purchaseMax || minimum < 0 || ledgerQuantity != quantity {
		return nil, purchaseFail(409, "Inventory quantity and costing are inconsistent")
	}
	newQuantity := quantity + delta
	units := delta
	if units < 0 {
		units = -units
	}
	// Positive adjustments can exceed the entire prior stock. Check the
	// rounded product before converting it back from arbitrary precision.
	scaled := new(big.Int).Mul(big.NewInt(cost), big.NewInt(units))
	scaled.Add(scaled, big.NewInt(quantity/2))
	scaled.Quo(scaled, big.NewInt(quantity))
	if !scaled.IsInt64() || scaled.Int64() > purchaseMax || newQuantity < 0 || newQuantity > purchaseMax {
		return nil, purchaseFail(409, "Adjusted inventory exceeds the supported range")
	}
	value := scaled.Int64()
	unitCost := roundedCost(value, units)
	if delta < 0 {
		value = -value
	}
	newCost := cost + value
	if newCost < 0 || newCost > purchaseMax || (newQuantity == 0 && newCost != 0) {
		return nil, purchaseFail(409, "Adjusted cost exceeds the supported range")
	}
	reference, version := "COUNT-"+token(), token()
	_, err = tx.ExecContext(ctx, `INSERT INTO erp_stock_ledger
		(id,organization_id,item_id,warehouse_id,change_type,quantity_change,balance_after,unit_cost,total_value,reference_doc)
		VALUES(?,?,?,?,'adjustment',?,?,?,?,?)`, version, org, in.ItemID, in.WarehouseID, delta, *in.Counted, unitCost, value, reference)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE erp_item_costing SET total_quantity=?,total_cost=?,moving_average_cost=?,updated_at=CURRENT_TIMESTAMP WHERE organization_id=? AND id=?",
		newQuantity, newCost, roundedCost(newCost, newQuantity), org, costID)
	if err != nil {
		return nil, err
	}
	result, _ := json.Marshal(map[string]any{"referenceDoc": reference, "balanceAfter": *in.Counted, "delta": delta, "version": version, "valueChange": value})
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)",
		token(), org, in.Key, endpoint, hash, string(result))
	if err != nil {
		return nil, err
	}
	before, _ := json.Marshal(map[string]any{"warehouseId": in.WarehouseID, "quantity": current.Quantity, "version": current.Version, "totalQuantity": quantity, "totalCost": cost})
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events
		(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,before_value,after_value,reason,request_id)
		VALUES(?,?,?,?,'erp.stock_count','erp_item',?,?,?,?,?)`,
		token(), org, id.UserID, grant.Role, in.ItemID, string(before), string(result), in.Reason, in.Key)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
