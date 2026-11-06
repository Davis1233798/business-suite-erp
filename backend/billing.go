package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type billingInput struct {
	Action      string `json:"action"`
	Key         string `json:"idempotencyKey"`
	InvoiceKind string `json:"invoiceKind"`
	OrderID     string `json:"orderId"`
	DueDate     string `json:"dueDate"`
	InvoiceID   string `json:"invoiceId"`
	Amount      int64  `json:"amount"`
	Method      string `json:"method"`
}

func (a *App) billing(w http.ResponseWriter, r *http.Request, id *Identity, grant *Grant) {
	if grant.Scope != "back" || (grant.Role != "operator" && grant.Role != "admin") {
		fail(w, 403, "Write-enabled back-office access required")
		return
	}
	var input billingInput
	if body(r, &input) != nil || len(input.Key) < 8 || len(input.Key) > 128 || len(input.OrderID) > 256 || len(input.InvoiceID) > 256 {
		fail(w, 400, "Invalid bounded finance request")
		return
	}
	switch input.Action {
	case "invoice":
		if input.OrderID == "" || (input.InvoiceKind != "payable" && input.InvoiceKind != "receivable") {
			fail(w, 400, "Select an invoice kind and source order")
			return
		}
		if input.DueDate != "" {
			date, err := time.Parse("2006-01-02", input.DueDate)
			if err != nil || date.Format("2006-01-02") != input.DueDate {
				fail(w, 400, "Enter a valid due date")
				return
			}
		}
	case "payment":
		if input.InvoiceID == "" || input.Amount < 1 || input.Amount > purchaseMax || (input.Method != "cash" && input.Method != "bank_transfer") {
			fail(w, 400, "Enter a valid payment amount and method")
			return
		}
	default:
		fail(w, 400, "Unknown finance operation")
		return
	}
	encoded, _ := json.Marshal(input)
	hash := digest(id.UserID + "\x00" + string(encoded))
	const endpoint = "/station/erp/finance"
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 503, "Finance unavailable")
		return
	}
	defer tx.Rollback()
	var oldHash, cached string
	err = tx.QueryRow("SELECT request_hash,response_body FROM idempotency_keys WHERE organization_id=? AND key=? AND endpoint=?", id.OrganizationID, input.Key, endpoint).Scan(&oldHash, &cached)
	if err == nil {
		if oldHash != hash {
			fail(w, 409, "Retry key already used for different input or account")
			return
		}
		var result map[string]any
		if json.Unmarshal([]byte(cached), &result) != nil {
			fail(w, 503, "Invalid stored finance result")
			return
		}
		ok(w, result)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 503, "Cannot read finance retry state")
		return
	}
	var result map[string]any
	if input.Action == "invoice" {
		result, err = createBillingInvoice(tx, id, input)
	} else {
		result, err = postBillingPayment(tx, id, input)
	}
	if err != nil {
		var denied *purchaseError
		if errors.As(err, &denied) {
			fail(w, denied.status, denied.message)
		} else {
			fail(w, 503, "Finance transaction failed; no changes were saved")
		}
		return
	}
	after, _ := json.Marshal(result)
	entity := input.InvoiceID
	if input.Action == "invoice" {
		entity = result["invoiceId"].(string)
	}
	_, err = tx.Exec("INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,after_value,request_id) VALUES(?,?,?,?,?,?,?,?,?)", token(), id.OrganizationID, id.UserID, grant.Role, "erp.finance."+input.Action, "ERP_INVOICE", entity, string(after), input.Key)
	if err == nil {
		_, err = tx.Exec("INSERT INTO idempotency_keys(id,organization_id,key,endpoint,request_hash,response_status,response_body) VALUES(?,?,?,?,?,200,?)", token(), id.OrganizationID, input.Key, endpoint, hash, string(after))
	}
	if err != nil {
		fail(w, 503, "Finance transaction failed; no changes were saved")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Cannot commit finance transaction")
		return
	}
	ok(w, result)
}

func createBillingInvoice(tx *sql.Tx, id *Identity, input billingInput) (map[string]any, error) {
	orderTable, lineTable, parent, numberColumn, partnerColumn, completeColumn, requiredStatus, partnerTable, prefix, partnerType :=
		"erp_purchase_orders", "erp_purchase_order_lines", "po_id", "po_number", "supplier_id", "received_quantity", "received", "erp_suppliers", "AP-", "supplier"
	if input.InvoiceKind == "receivable" {
		orderTable, lineTable, parent, numberColumn, partnerColumn, completeColumn, requiredStatus, partnerTable, prefix, partnerType =
			"erp_sales_orders", "erp_sales_order_lines", "so_id", "so_number", "customer_id", "shipped_quantity", "shipped", "erp_customers", "AR-", "customer"
	}
	var number, partner, status string
	var storedTotal int64
	err := tx.QueryRow("SELECT "+numberColumn+","+partnerColumn+",status,total_amount FROM "+orderTable+" WHERE organization_id=? AND id=?", id.OrganizationID, input.OrderID).Scan(&number, &partner, &status, &storedTotal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, purchaseFail(404, "Source order not found")
	}
	if err != nil {
		return nil, err
	}
	if status != requiredStatus {
		return nil, purchaseFail(409, "Order must be fully received or shipped before invoicing")
	}
	var partners int
	if err = tx.QueryRow("SELECT count(*) FROM "+partnerTable+" WHERE organization_id=? AND id=?", id.OrganizationID, partner).Scan(&partners); err != nil {
		return nil, err
	}
	if partners != 1 {
		return nil, purchaseFail(409, "Source partner is unavailable in this organization")
	}
	rows, err := tx.Query("SELECT quantity,unit_price,"+completeColumn+" FROM "+lineTable+" WHERE organization_id=? AND "+parent+"=?", id.OrganizationID, input.OrderID)
	if err != nil {
		return nil, err
	}
	var total int64
	count := 0
	for rows.Next() {
		var quantity, price, completed int64
		if err = rows.Scan(&quantity, &price, &completed); err != nil {
			rows.Close()
			return nil, err
		}
		if quantity <= 0 || quantity > purchaseMax || price < 0 || price > purchaseMax || completed != quantity || (price != 0 && quantity > purchaseMax/price) {
			rows.Close()
			return nil, purchaseFail(409, "Source lines are incomplete or exceed supported money limits")
		}
		lineTotal := quantity * price
		if total > purchaseMax-lineTotal {
			rows.Close()
			return nil, purchaseFail(409, "Invoice total exceeds supported range")
		}
		total += lineTotal
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if count == 0 || total <= 0 || total != storedTotal {
		return nil, purchaseFail(409, "Source order total is inconsistent with its completed lines")
	}
	entity := "invoice_" + digest(id.OrganizationID+"\x00"+input.InvoiceKind+"\x00"+input.OrderID)
	invoiceNumber := prefix + number
	var exists int
	if err = tx.QueryRow("SELECT count(*) FROM erp_invoices WHERE organization_id=? AND (id=? OR (invoice_type=? AND invoice_number=?))", id.OrganizationID, entity, input.InvoiceKind, invoiceNumber).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 0 {
		return nil, purchaseFail(409, "An invoice already exists for this order")
	}
	var due any
	if input.DueDate != "" {
		due = input.DueDate
	}
	_, err = tx.Exec("INSERT INTO erp_invoices(id,organization_id,invoice_number,invoice_type,partner_type,partner_id,total_amount,due_date,status) VALUES(?,?,?,?,?,?,?,?,'unpaid')", entity, id.OrganizationID, invoiceNumber, input.InvoiceKind, partnerType, partner, total, due)
	if err != nil {
		return nil, err
	}
	return map[string]any{"invoiceId": entity, "invoiceNumber": invoiceNumber, "totalAmount": total}, nil
}

func postBillingPayment(tx *sql.Tx, id *Identity, input billingInput) (map[string]any, error) {
	var total, paid int64
	var status string
	err := tx.QueryRow("SELECT total_amount,paid_amount,status FROM erp_invoices WHERE organization_id=? AND id=?", id.OrganizationID, input.InvoiceID).Scan(&total, &paid, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, purchaseFail(404, "Invoice not found")
	}
	if err != nil {
		return nil, err
	}
	if total <= 0 || total > purchaseMax || paid < 0 || paid > total || (status != "unpaid" && status != "partially_paid") || input.Amount > total-paid {
		return nil, purchaseFail(409, "Payment exceeds balance or invoice is closed")
	}
	newPaid := paid + input.Amount
	next := "partially_paid"
	if newPaid == total {
		next = "paid"
	}
	changed, err := tx.Exec("UPDATE erp_invoices SET paid_amount=?,status=? WHERE organization_id=? AND id=? AND paid_amount=? AND status=?", newPaid, next, id.OrganizationID, input.InvoiceID, paid, status)
	if err != nil {
		return nil, err
	}
	n, err := changed.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, purchaseFail(409, "Invoice balance changed concurrently")
	}
	number := "PAY-" + strings.ToUpper(token())
	_, err = tx.Exec("INSERT INTO erp_payments(id,organization_id,payment_number,invoice_id,amount,payment_method) VALUES(?,?,?,?,?,?)", token(), id.OrganizationID, number, input.InvoiceID, input.Amount, input.Method)
	if err != nil {
		return nil, err
	}
	return map[string]any{"paymentNumber": number, "paidAmount": newPaid, "unpaidAmount": total - newPaid, "status": next}, nil
}
