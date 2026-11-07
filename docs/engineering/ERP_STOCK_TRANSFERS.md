# ERP warehouse transfer transaction

## Architecture

The independent ERP owns purchase receipts, shipments and item costing, but
has no persisted warehouse-transfer workflow. Implement transfers in the Go
service and its React back office. Do not proxy to portfolio TypeScript.

POST `/api/stations/erp/inventory` accepts action `transfer`, `itemId`,
`fromWarehouseId`, `toWarehouseId`, positive integer `quantity`, and `requestId`.
The session supplies organization and actor. Only enabled write-capable ERP
back-office grants may transfer; source/destination/item must belong to that
organization, the item must be active, and warehouses must differ.

## Program boundaries and invariants

- Read the current source and destination balances from the persisted ledger
  inside the transaction. Do not trust a browser's 500-row query window as a
  complete stock balance. Provide an authorized, bounded selected-item stock
  lookup for the form instead of deriving availability from incomplete rows.
- Reject insufficient stock, invalid/overflowing quantities, inconsistent
  global quantity/cost and any destination balance exceeding the safe integer
  bound. Quantities and minor-unit values stay in 0..9007199254740991.
- Insert one `transfer_out` and one `transfer_in` ledger row under a shared
  transfer reference. Their quantity and total-value deltas must cancel
  exactly. Allocate the moved value using the existing exact proportional
  `allocatedCost` helper; retain the item's total quantity, total cost and
  moving-average cost unchanged. A transfer relocates stock rather than
  consuming inventory value.
- Both ledger rows, structured before/after audit and actor/payload-bound
  retry result commit atomically. A failed second ledger, audit or retry insert
  must roll back all prior writes. Repeating a committed intent returns its
  result; changing input under the same key conflicts.
- Concurrent transfers may not overdraw the source. React keeps failed input
  and uncertain retry keys, uses fresh server availability, supports English
  and Traditional Chinese, and hides write controls for read-only/front roles.
  Preserve purchase, shipment, invoice, journal, pagination and print behavior.

## Tests before implementation

Use actual HTTP purchase approval/receipts to prepare stock. Cover persisted
balances, exact value conservation, same-key replay and changed-key conflicts,
tenant/role denial, insufficient stock, simultaneous withdrawals and late
transaction rollback. Add real Chromium transfer/reload, mobile, localization
and read-only acceptance before stability review. GitHub CI runs all tests and
builds; no local application tests/builds run.

Stock counting, returns and fiscal close remain separate unfinished ERP flows.
This transfer iteration does not establish independent OCI or Windows runtime
delivery.
