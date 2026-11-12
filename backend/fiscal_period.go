package main

import (
	"net/http"
)

func (a *App) fiscalPeriods(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	org := id.OrganizationID
	if r.Method == http.MethodGet {
		periods, err := queryMaps(a.db, "SELECT id,period_code,start_date,end_date,is_closed FROM erp_fiscal_periods WHERE organization_id=? ORDER BY start_date,id", org)
		if err != nil {
			fail(w, 500, "Unable to list fiscal periods")
			return
		}
		for _, period := range periods {
			period["isClosed"] = period["isClosed"] == int64(1) || period["isClosed"] == true
		}
		ok(w, periods)
		return
	}
	period := r.PathValue("period")
	if r.Method != http.MethodPost || period == "" {
		fail(w, 405, "Method not allowed")
		return
	}
	if g.Scope != "back" || (g.Role != "admin" && g.Role != "operator") {
		fail(w, 403, "Back-office posting permission required")
		return
	}
	ctx := r.Context()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 500, "Unable to begin fiscal period close")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE erp_fiscal_periods SET is_closed=1 WHERE organization_id=? AND id=? AND is_closed=0", org, period)
	if err != nil {
		fail(w, 500, "Unable to close fiscal period")
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		fail(w, 500, "Unable to verify fiscal period update")
		return
	}
	if changed == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM erp_fiscal_periods WHERE organization_id=? AND id=?", org, period).Scan(&exists); err != nil {
			fail(w, 500, "Unable to verify fiscal period")
			return
		}
		if exists == 0 {
			fail(w, 404, "Fiscal period not found")
			return
		}
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_events(id,organization_id,actor_id,actor_role,action,entity_type,entity_id,before_value,after_value) VALUES(?,?,?,?,'ERP_FISCAL_PERIOD_CLOSE','erp_fiscal_period',?,'{\"isClosed\":false}','{\"isClosed\":true}')", token(), org, id.UserID, g.Role, period)
		if err != nil {
			fail(w, 500, "Unable to audit fiscal period close")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "Unable to commit fiscal period close")
		return
	}
	ok(w, map[string]any{"id": period, "isClosed": true})
}
