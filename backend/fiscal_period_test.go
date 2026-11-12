package main

import (
	"strings"
	"testing"
)

func TestFiscalPeriodClose(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	viewer := login(t, a, "viewer@suite.test", space)
	other := login(t, a, "admin@suite.test", token())
	org := journalOrg(t, a, admin)
	period := token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date) VALUES(?,?,?,?,?)", period, org, "test-close", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}
	base := "/api/stations/erp/fiscal-periods"
	closePath := base + "/" + period + "/close"
	if w := request(a, "POST", closePath, nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := request(a, "POST", closePath, nil, viewer...); w.Code != 403 {
		t.Fatalf("viewer: %d", w.Code)
	}
	if w := request(a, "POST", closePath, nil, other...); w.Code != 404 {
		t.Fatalf("other organization: %d", w.Code)
	}
	if w := request(a, "GET", base, nil, other...); w.Code != 200 || strings.Contains(w.Body.String(), period) {
		t.Fatalf("other list: %d %s", w.Code, w.Body)
	}
	for i := 0; i < 2; i++ {
		if w := request(a, "POST", closePath, nil, admin...); w.Code != 200 {
			t.Fatalf("close %d: %d %s", i, w.Code, w.Body)
		}
	}
	var count int
	if err := a.db.QueryRow("SELECT count(*) FROM audit_events WHERE organization_id=? AND entity_id=? AND action='ERP_FISCAL_PERIOD_CLOSE'", org, period).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count: %d %v", count, err)
	}
	if w := request(a, "GET", base, nil, viewer...); w.Code != 200 || !strings.Contains(w.Body.String(), period) || !strings.Contains(w.Body.String(), `"isClosed":true`) {
		t.Fatalf("closed period missing: %d %s", w.Code, w.Body)
	}
	for date, expected := range map[string]int{"2027-02-01": 409, "2027-02-28": 409, "2027-03-01": 200} {
		input := journalInput()
		input["entryDate"] = date
		if w := request(a, "POST", "/api/stations/erp/journal", input, admin...); w.Code != expected {
			t.Fatalf("%s: %d %s", date, w.Code, w.Body)
		}
	}
}

func TestFiscalPeriodCloseRollsBackOnAuditFailure(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org, period := journalOrg(t, a, admin), token()
	if _, err := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date) VALUES(?,?,?,?,?)", period, org, "rollback", "2027-02-01", "2027-02-28"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_period_audit BEFORE INSERT ON audit_events WHEN NEW.action='ERP_FISCAL_PERIOD_CLOSE' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if w := request(a, "POST", "/api/stations/erp/fiscal-periods/"+period+"/close", nil, admin...); w.Code != 500 {
		t.Fatalf("close: %d %s", w.Code, w.Body)
	}
	var closed bool
	if err := a.db.QueryRow("SELECT is_closed FROM erp_fiscal_periods WHERE id=?", period).Scan(&closed); err != nil || closed {
		t.Fatalf("period close was not rolled back: %t %v", closed, err)
	}
}
