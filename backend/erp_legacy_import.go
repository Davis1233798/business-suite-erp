package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func importLegacyERP(source, destination, organization string) error {
	if strings.TrimSpace(organization) == "" {
		return fmt.Errorf("organization is required")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return fmt.Errorf("destination must be new")
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".erp-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	file := filepath.Join(staging, "erp.sqlite")
	db, err := openDB(file)
	if err != nil {
		return err
	}
	defer db.Close()
	path := filepath.ToSlash(source)
	if len(filepath.VolumeName(source)) == 2 {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	if _, err := db.Exec("ATTACH DATABASE ? AS legacy", uri.String()); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	inserted, err := tx.Exec("INSERT INTO organizations(id,name,slug,created_at) SELECT id,name,slug,created_at FROM legacy.organizations WHERE id=?", organization)
	if err != nil {
		return err
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("organization not found")
	}
	tables := []string{
		"erp_customers", "erp_fiscal_periods", "erp_items", "erp_suppliers", "erp_warehouses",
		"erp_item_costing", "erp_purchase_orders", "erp_purchase_order_lines", "erp_receipts",
		"erp_receipt_lines", "erp_sales_orders", "erp_sales_order_lines", "erp_shipments",
		"erp_shipment_lines", "erp_stock_ledger", "erp_invoices", "erp_payments",
		"erp_journal_entries", "erp_journal_lines",
	}
	for _, table := range tables {
		var extendedCost int
		if table == "erp_shipment_lines" {
			if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info(?, 'legacy') WHERE name='extended_cost'", table).Scan(&extendedCost); err != nil {
				return err
			}
		}
		rows, err := tx.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
		if err != nil {
			return err
		}
		var columns []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				rows.Close()
				return err
			}
			if table == "erp_shipment_lines" && column == "extended_cost" && extendedCost == 0 {
				continue
			}
			columns = append(columns, `"`+strings.ReplaceAll(column, `"`, `""`)+`"`)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		list := strings.Join(columns, ",")
		query := fmt.Sprintf("INSERT INTO main.%s (%s) SELECT %s FROM legacy.%s WHERE organization_id=?", table, list, list, table)
		if table == "erp_fiscal_periods" {
			continue
		}
		if _, err := tx.Exec(query, organization); err != nil {
			return fmt.Errorf("import %s: %w", table, err)
		}
		if table == "erp_shipment_lines" && extendedCost == 0 {
			if err := backfillShipmentCost(tx, organization); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec("INSERT INTO main.erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed,created_at) SELECT id,organization_id,period_code,start_date,end_date,is_closed,created_at FROM legacy.erp_fiscal_periods WHERE organization_id=?", organization); err != nil {
		return err
	}
	var invalidJournals int
	if err := tx.QueryRow(`SELECT count(*) FROM (
		SELECT journal_id FROM erp_journal_lines
		LEFT JOIN erp_journal_entries ON erp_journal_entries.id=erp_journal_lines.journal_id
			AND erp_journal_entries.organization_id=erp_journal_lines.organization_id
		GROUP BY journal_id
		HAVING count(erp_journal_entries.id)<>count(*) OR sum(debit_amount)<>sum(credit_amount)
	)`).Scan(&invalidJournals); err != nil {
		return fmt.Errorf("reconcile journal: %w", err)
	}
	if invalidJournals != 0 {
		return fmt.Errorf("legacy ERP contains orphaned or unbalanced journal lines")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return os.Link(file, destination)
}
