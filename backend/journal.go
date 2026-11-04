package main

import (
	"net/http"
	"strings"
	"time"
)

type journalLine struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Debit       int64  `json:"debitAmount"`
	Credit      int64  `json:"creditAmount"`
}

func (a *App) postJournal(w http.ResponseWriter, r *http.Request, id *Identity, g *Grant) {
	if g.Scope != "back" || (g.Role != "admin" && g.Role != "operator") {
		fail(w, 403, "Back-office posting permission required")
		return
	}
	var input struct {
		EntryDate   string        `json:"entryDate"`
		Description string        `json:"description"`
		Lines       []journalLine `json:"lines"`
	}
	if body(r, &input) != nil {
		fail(w, 400, "Invalid journal input")
		return
	}
	date, e := time.Parse("2006-01-02", input.EntryDate)
	input.Description = strings.TrimSpace(input.Description)
	if e != nil || date.Format("2006-01-02") != input.EntryDate || input.Description == "" || len(input.Description) > 500 || len(input.Lines) < 2 || len(input.Lines) > 100 {
		fail(w, 400, "Provide a valid date, description and 2-100 lines")
		return
	}
	var debit, credit int64
	for i := range input.Lines {
		l := &input.Lines[i]
		l.AccountCode = strings.TrimSpace(l.AccountCode)
		l.AccountName = strings.TrimSpace(l.AccountName)
		if l.AccountCode == "" || len(l.AccountCode) > 200 || l.AccountName == "" || len(l.AccountName) > 500 || l.Debit < 0 || l.Credit < 0 || l.Debit > 10000000 || l.Credit > 10000000 || ((l.Debit > 0) == (l.Credit > 0)) {
			fail(w, 400, "Each line requires one positive debit or credit and an account")
			return
		}
		debit += l.Debit
		credit += l.Credit
	}
	if debit != credit || debit == 0 {
		fail(w, 400, "Debits and credits must balance to a positive total")
		return
	}
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Unable to begin journal posting")
		return
	}
	defer tx.Rollback()
	var closed int
	e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM erp_fiscal_periods WHERE organization_id=? AND is_closed=1 AND start_date<=? AND end_date>=?", id.OrganizationID, input.EntryDate, input.EntryDate).Scan(&closed)
	if e != nil {
		fail(w, 500, "Unable to verify fiscal period")
		return
	}
	if closed > 0 {
		fail(w, 409, "Fiscal period is closed")
		return
	}
	entryID := "je_" + token()
	number := "JE-" + token()
	_, e = tx.ExecContext(r.Context(), "INSERT INTO erp_journal_entries(id,organization_id,entry_number,entry_date,description,status) VALUES(?,?,?,?,?,'posted')", entryID, id.OrganizationID, number, input.EntryDate, input.Description)
	if e != nil {
		fail(w, 500, "Unable to post journal")
		return
	}
	for _, l := range input.Lines {
		_, e = tx.ExecContext(r.Context(), "INSERT INTO erp_journal_lines(id,organization_id,journal_id,account_code,account_name,debit_amount,credit_amount) VALUES(?,?,?,?,?,?,?)", "jel_"+token(), id.OrganizationID, entryID, l.AccountCode, l.AccountName, l.Debit, l.Credit)
		if e != nil {
			fail(w, 500, "Unable to post journal lines")
			return
		}
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "Unable to commit journal")
		return
	}
	ok(w, map[string]any{"id": entryID, "entryNumber": number, "totalDebit": debit, "totalCredit": credit})
}
