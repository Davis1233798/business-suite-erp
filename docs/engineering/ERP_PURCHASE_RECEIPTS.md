# ERP purchase approval and inventory receipt

## Architecture

Port the purchase lifecycle to Go and its shipped React workspace. Use one POST /api/stations/erp/purchases action endpoint (`create`, `approve`, `reject`, `receive`). No proxy to TypeScript and no simulated success. Query/print continue using persisted ERP datasets. This follows journal posting; sales, invoices, returns and reconciliation remain separate unfinished flows.

## Program boundaries

Resolve tenant, actor and current grant from the server session on every request. Create/receive require back-office operator/admin; approve/reject require back-office admin. Browser tenant/actor/role selectors are rejected. All supplier, warehouse, item, order and line lookups include organization scope. Only active suppliers/items may create a new order.

Creation validates 1-100 distinct item lines, positive bounded integer quantities and nonnegative integer minor-unit prices; calculate totals on the server with safe-integer bounds. Create a draft header and lines atomically. Approval/rejection conditionally changes only a draft order; rejection needs a nonempty reason. Persist actor, before/after state and reason in audit_events in the same transaction.

Receive only approved orders into an existing same-tenant warehouse. Reject missing, duplicate, foreign or wrong-order line IDs. Convert boxes using the persisted item's positive conversion ratio, bound the resulting pieces and prevent over-receipt with conditional SQL updates. Create receipt header/lines, append correctly balanced stock-ledger entries, update moving-average costing and order status, and append audit together in one SQLite transaction. Repeated same-item receipts in a transaction must use the latest balance and cost. Full receipt changes order status to received; partial receipt keeps approved. No partial header, quantity or cost changes may remain on any failure.

Each mutation requires a bounded client requestId. Successful replay with the same canonical payload returns the stored result, without repeating stock/cost changes. Reusing the key for different input returns conflict. Scope keys by tenant, actor and endpoint, and insert the replay result inside the business transaction. Check current permissions before replay. Use the existing durable idempotency_keys table, not process memory.

React controls use actual same-tenant datasets, show authoritative state and remaining quantity, preserve rejected input, reuse the requestId after network failure and refresh data after success. Provide English and Traditional Chinese. Read-only/front-office views do not show write controls; the server remains authoritative.

## Tests before implementation

Commit remote HTTP+SQLite tests before code: actual create/approve/partial/full receive; admin-only decisions; anonymous/front/read-only denial; foreign tenant supplier/warehouse/line denial; box conversion; duplicate/over-receipt and concurrent receipts; exact idempotent replay and payload mismatch; moving-average/stock balance; forced late failure rollback across all touched tables; audit actor and transitions. Add remote Chromium acceptance for the complete persisted UI flow and retained rejection input. No local application tests/builds.

## Release and remaining evidence

Exact-head Linux race tests, TypeScript, Chromium, image and Windows package checks must pass before autonomous review and main merge. A published image is not an OCI cutover; Windows CI packaging is not a verified installation on the user's computer. Keep those acceptance states separate in the portfolio's development status.