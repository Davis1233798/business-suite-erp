# ERP physical stock count

Port the physical stock count flow from the portfolio ERP demo into the
independent ERP application. An administrator enters the observed quantity
for an item and warehouse and records a reason for the discrepancy.

## Program boundaries

- Back-office administrators can read the current count snapshot and submit
  an adjustment through `/api/stations/erp/stock-count`. Require an active
  item and warehouse in the authenticated tenant.
- Read the latest ledger quantity and record ID for the selected warehouse.
  The submission must match both, including when intervening writes returned
  the balance to the same number. Reject unchanged counts.
- Read costing and current balances across all warehouses from persisted
  records in the same transaction. Reconcile quantity before applying the
  difference. Value the difference at the existing exact average, rounded
  once to a minor currency unit, and reject quantity or cost overflow.
- Append one adjustment ledger row and atomically update costing, audit the
  reason and store the idempotent response. A failed write leaves these
  records unchanged. The client keeps the original request after an
  uncertain result and fetches fresh availability after success.
- Display the latest count, version conflict, reason, and committed
  reference in English and Traditional Chinese. Keep count approval inside
  the existing ERP back office.

## Remote acceptance

Use actual receipt and count APIs in fixtures. Check exact quantity and
cost, tenant and role denial, stale version, concurrent approval, identical
retry and changed-payload conflict, and rollback after a late audit error.
Exercise the back-office flow in Chromium with a fresh data fetch and check
the mobile view. GitHub Actions runs the tests and builds.
