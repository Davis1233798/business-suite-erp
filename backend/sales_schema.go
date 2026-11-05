package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func migrateShipmentCost(tx *sql.Tx) error {
	rows, err := tx.Query("PRAGMA table_info(erp_shipment_lines)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var ordinal, required, primary int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&ordinal, &name, &kind, &required, &defaultValue, &primary); err != nil {
			rows.Close()
			return err
		}
		if name == "extended_cost" {
			if !strings.EqualFold(kind, "INTEGER") {
				rows.Close()
				return errors.New("shipment extended cost has an unsupported type")
			}
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	if _, err = tx.Exec("ALTER TABLE erp_shipment_lines ADD COLUMN extended_cost INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return backfillShipmentCost(tx, "")
}

func backfillShipmentCost(tx *sql.Tx, organization string) error {
	filter := "extended_cost=0"
	args := []any{}
	if organization != "" {
		filter += " AND organization_id=?"
		args = append(args, organization)
	}
	var invalid string
	query := "SELECT id FROM erp_shipment_lines WHERE (" + filter + ") AND (typeof(quantity)!='integer' OR typeof(unit_cost)!='integer' OR quantity<0 OR unit_cost<0 OR quantity>9007199254740991 OR unit_cost>9007199254740991 OR (unit_cost>0 AND quantity>9007199254740991/unit_cost)) LIMIT 1"
	err := tx.QueryRow(query, args...).Scan(&invalid)
	if err == nil {
		return fmt.Errorf("legacy shipment cost exceeds the supported exact integer range; migration refused")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec("UPDATE erp_shipment_lines SET extended_cost=quantity*unit_cost WHERE "+filter, args...)
	return err
}
