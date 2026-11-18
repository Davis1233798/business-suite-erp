package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

var taipeiZone = time.FixedZone("Asia/Taipei", 8*3600)

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// parseAccountingDates converts a raw date string into one or two accounting dates (YYYY-MM-DD).
// For timestamps, it evaluates both the Taipei time (UTC+8) and the standard UTC time boundaries.
func parseAccountingDates(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		now := time.Now()
		tTaipei := now.In(taipeiZone).Format("2006-01-02")
		tUTC := now.UTC().Format("2006-01-02")
		if tTaipei == tUTC {
			return []string{tTaipei}, nil
		}
		return []string{tTaipei, tUTC}, nil
	}
	// Direct YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", raw); err == nil && t.Format("2006-01-02") == raw {
		return []string{raw}, nil
	}
	// Try RFC3339 / ISO formats
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			tTaipei := t.In(taipeiZone).Format("2006-01-02")
			tUTC := t.UTC().Format("2006-01-02")
			if tTaipei == tUTC {
				return []string{tTaipei}, nil
			}
			return []string{tTaipei, tUTC}, nil
		}
	}
	return nil, fmt.Errorf("invalid date format: %s", raw)
}

// checkFiscalPeriodClosed validates whether any of the accounting dates corresponding to rawDate
// falls within a closed fiscal period for the organization.
// Returns a 409 purchaseError if closed, or an error if query fails.
func checkFiscalPeriodClosed(ctx context.Context, q queryRower, org string, rawDate string) error {
	dates, err := parseAccountingDates(rawDate)
	if err != nil {
		return purchaseFail(400, "Invalid date format")
	}
	for _, d := range dates {
		var closed int
		err := q.QueryRowContext(ctx, "SELECT count(*) FROM erp_fiscal_periods WHERE organization_id=? AND (is_closed=1 OR status='closed') AND start_date<=? AND end_date>=?", org, d, d).Scan(&closed)
		if err != nil && strings.Contains(err.Error(), "no such column: status") {
			err = q.QueryRowContext(ctx, "SELECT count(*) FROM erp_fiscal_periods WHERE organization_id=? AND is_closed=1 AND start_date<=? AND end_date>=?", org, d, d).Scan(&closed)
		}
		if err != nil {
			return err
		}
		if closed > 0 {
			return purchaseFail(409, "Fiscal period is closed")
		}
	}
	return nil
}
