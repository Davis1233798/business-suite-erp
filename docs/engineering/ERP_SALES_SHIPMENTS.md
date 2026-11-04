# ERP sales and warehouse shipment flow

## Architecture and program boundaries

Port sales creation, confirmation and partial/full shipment into the Go ERP service and shipped React workspace. POST `/api/stations/erp/sales` accepts `create`, `confirm` and `ship` actions. Resolve organization and actor from the current session; only enabled back-office operators/administrators may mutate. Reject browser identity selectors and scope customer, item, order, line and warehouse lookups to that organization. No proxy to TypeScript or simulated success.

Create a draft with 1-100 distinct active items, positive bounded integer quantities and nonnegative integer minor-unit prices. Compute safe-integer totals on the server. Confirmation changes only a draft to confirmed. Ship only confirmed/partially-shipped orders, never cancelled, draft or completed orders. Require unique same-order line IDs and a same-tenant warehouse. Conditional quantity updates and the existing stock-ledger trigger prevent over-shipment or negative warehouse stock.

Use one transaction for shipment header/lines, order counters/status, warehouse stock, global moving-average inventory costing, audit and durable replay result. Preserve exact extended shipment cost in an additive `extended_cost` shipment-line column, retaining rounded `unit_cost` for compatibility. Allocate global inventory cost proportionally with integer arithmetic, taking the exact remaining cost on final depletion; never manufacture stock/cost or use floating point. A failed validation, insertion or audit must leave every touched table unchanged.

Migrate existing repository databases additively and idempotently: discover the column before alteration, preserve all existing records, and backfill legacy extended cost from their recorded quantity/unit cost with overflow validation. Verify reopen and failed-upgrade behavior. Do not read or adopt the old shared suite database automatically.

All mutations need a bounded client request ID. Scope successful replay by tenant, actor and endpoint, compare canonical payload hashes and recheck current permissions before replay. Different payload with the same key conflicts. Exact replays must not deduct inventory twice. UI retains an identical request ID after a network failure, preserves rejected input, refreshes persisted datasets and supports English/Traditional Chinese. Front/read-only views have no mutation controls; server checks remain authoritative.

## Tests before implementation

Commit remote HTTP/SQLite tests before code: real create/confirm/partial/full ship, warehouse/global quantity and cost reconciliation, replay and mismatch, over-shipment, insufficient warehouse stock, duplicate/foreign lines and foreign customer/warehouse, anonymous/front/read-only denial, concurrent stock competition, forced late failure rollback, exact cost rounding/depletion and additive migration/reopen. Extend remote Chromium to run an actual purchase receipt followed by sales shipment and check persisted records plus rejected-input retention. No local application tests/builds.

## Release and remaining scope

Require exact-head Linux/race, TypeScript, browser, container and Windows checks, autonomous review and main merge. OCI cutover and actual Windows client-server acceptance remain separate. Invoicing/payment, returns, stock count/transfer, period close and remaining ERP workflows are not claimed complete by this flow.
