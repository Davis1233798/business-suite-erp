package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func journalInput() map[string]any {
	return map[string]any{"entryDate": "2027-02-04", "description": "Test journal", "lines": []map[string]any{{"accountCode": "1100", "accountName": "Cash", "debitAmount": 1200, "creditAmount": 0}, {"accountCode": "4100", "accountName": "Revenue", "debitAmount": 0, "creditAmount": 1200}}}
}
func journalOrg(t *testing.T, a *App, c []*http.Cookie) string {
	t.Helper()
	w := request(a, "GET", "/api/auth/me", nil, c...)
	var out struct {
		Data Identity `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out.Data.OrganizationID
}
func TestJournalPostingAuthorizationPersistenceIsolation(t *testing.T) {
	a := appForTest(t)
	space := token()
	admin := login(t, a, "admin@suite.test", space)
	org := journalOrg(t, a, admin)
	for _, email := range []string{"viewer@suite.test", "front@suite.test"} {
		c := login(t, a, email, space)
		if w := request(a, "POST", "/api/stations/erp/journal", journalInput(), c...); w.Code != 403 {
			t.Fatalf("%s: %d %s", email, w.Code, w.Body)
		}
	}
	if w := request(a, "POST", "/api/stations/erp/journal", journalInput()); w.Code != 401 {
		t.Fatal(w.Code)
	}
	tampered := journalInput()
	tampered["organizationId"] = "attacker"
	if w := request(a, "POST", "/api/stations/erp/journal", tampered, admin...); w.Code != 400 {
		t.Fatal("browser tenant selector accepted", w.Code)
	}
	input := journalInput()
	operator := login(t, a, "office@suite.test", space)
	w := request(a, "POST", "/api/stations/erp/journal", input, operator...)
	if w.Code != 200 {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Data.ID == "" {
		t.Fatal("missing persisted id")
	}
	var count, debit, credit int
	if e := a.db.QueryRow("SELECT count(*),sum(debit_amount),sum(credit_amount) FROM erp_journal_lines WHERE organization_id=? AND journal_id=?", org, out.Data.ID).Scan(&count, &debit, &credit); e != nil || count != 2 || debit != 1200 || credit != 1200 {
		t.Fatalf("ledger: %d %d %d %v", count, debit, credit, e)
	}
	other := login(t, a, "admin@suite.test", token())
	view := request(a, "GET", "/api/stations/erp/data", nil, other...)
	if strings.Contains(view.Body.String(), out.Data.ID) {
		t.Fatal("cross organization journal leaked")
	}
}
func TestJournalRejectsInvalidInputWithoutWrites(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org := journalOrg(t, a, admin)
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"date", func(p map[string]any) { p["entryDate"] = "2027-02-30" }},
		{"description", func(p map[string]any) { p["description"] = " " }},
		{"negative", func(p map[string]any) { p["lines"].([]map[string]any)[0]["debitAmount"] = -1 }},
		{"fractional", func(p map[string]any) { p["lines"].([]map[string]any)[0]["debitAmount"] = 1.5 }},
		{"both sides", func(p map[string]any) { p["lines"].([]map[string]any)[0]["creditAmount"] = 1 }},
		{"imbalance", func(p map[string]any) { p["lines"].([]map[string]any)[0]["debitAmount"] = 1 }},
		{"zero", func(p map[string]any) {
			for _, l := range p["lines"].([]map[string]any) {
				l["debitAmount"] = 0
				l["creditAmount"] = 0
			}
		}},
		{"too many", func(p map[string]any) {
			lines := []map[string]any{}
			for i := 0; i < 101; i++ {
				lines = append(lines, p["lines"].([]map[string]any)[0])
			}
			p["lines"] = lines
		}},
	}
	var before int
	a.db.QueryRow("SELECT count(*) FROM erp_journal_entries WHERE organization_id=?", org).Scan(&before)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := journalInput()
			c.mutate(p)
			w := request(a, "POST", "/api/stations/erp/journal", p, admin...)
			if w.Code != 400 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	var after int
	a.db.QueryRow("SELECT count(*) FROM erp_journal_entries WHERE organization_id=?", org).Scan(&after)
	if before != after {
		t.Fatal("invalid journal changed ledger")
	}
}
func TestJournalClosedPeriodAndLateFailureAreAtomic(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org := journalOrg(t, a, admin)
	if _, e := a.db.Exec("INSERT INTO erp_fiscal_periods(id,organization_id,period_code,start_date,end_date,is_closed) VALUES(?,?,?,?,?,1)", token(), org, "closed-test", "2027-02-01", "2027-02-28"); e != nil {
		t.Fatal(e)
	}
	if w := request(a, "POST", "/api/stations/erp/journal", journalInput(), admin...); w.Code != 409 {
		t.Fatalf("closed %d %s", w.Code, w.Body)
	}
	if _, e := a.db.Exec("CREATE TRIGGER reject_test_journal_line BEFORE INSERT ON erp_journal_lines WHEN NEW.account_code='4100' BEGIN SELECT RAISE(ABORT,'forced late failure'); END"); e != nil {
		t.Fatal(e)
	}
	var before int
	a.db.QueryRow("SELECT count(*) FROM erp_journal_entries WHERE organization_id=?", org).Scan(&before)
	p := journalInput()
	p["entryDate"] = "2027-03-04"
	if w := request(a, "POST", "/api/stations/erp/journal", p, admin...); w.Code != 500 {
		t.Fatalf("late failure %d %s", w.Code, w.Body)
	}
	var after int
	a.db.QueryRow("SELECT count(*) FROM erp_journal_entries WHERE organization_id=?", org).Scan(&after)
	if after != before {
		t.Fatal("partial header committed")
	}
}

func TestJournalNumberUniquenessWithinOrganization(t *testing.T) {
	a := appForTest(t)
	admin := login(t, a, "admin@suite.test", token())
	org := journalOrg(t, a, admin)
	query := "INSERT INTO erp_journal_entries(id,organization_id,entry_number,entry_date,description) VALUES(?,?,?,'2027-03-04','Unique number')"
	if _, e := a.db.Exec(query, token(), org, "JE-duplicate-test"); e != nil {
		t.Fatal(e)
	}
	if _, e := a.db.Exec(query, token(), org, "JE-duplicate-test"); e == nil {
		t.Fatal("duplicate journal number accepted")
	}
}
