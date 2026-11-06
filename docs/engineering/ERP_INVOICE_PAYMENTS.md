# ERP linked invoices and payment posting

## Architecture

The independent Go ERP service must own internal accounts payable/receivable
and payment records. The shared portfolio already has a finance workflow;
its source is a reference, not a runtime dependency. Existing independent ERP
purchase, sales, shipment, costing and journal handlers retain their ownership.

Expose POST `/api/stations/erp/finance` with `invoice` and `payment` actions.
Use the existing authenticated organization and write-enabled back-office grant.
The request carries an `idempotencyKey` for an actor-bound, payload-bound retry.
These are internal ledger records; no electronic tax invoice or bank transfer
is issued. Journal entry posting remains in its existing handler.

## Program boundaries and invariants

- Invoice input: `invoiceKind` payable/receivable, `orderId`, optional strict
  ISO calendar `dueDate`, and retry key. Only a same-organization, fully
  received purchase or fully shipped sale with positive quantities and exact
  integer line totals can produce an invoice. Reject cancelled/rejected or
  incomplete orders, invalid partner references and inconsistent totals.
- Compute and validate money using bounded integer arithmetic; do not trust
  client totals or use floating-point calculations. Enforce one invoice per
  source order under different concurrent request keys, using a deterministic
  organization/kind/source identity and database constraints.
- Payment input: invoice ID, positive integer amount and cash/bank_transfer
  method. Atomically update the unpaid/partially_paid invoice and insert one
  payment record. Reject overpayment, foreign references and closed invoices.
- Invoice/payment state, structured audit and retry result commit in one
  transaction. Late audit/retry failures roll back every business mutation.
  Replays return the committed result without duplicate payments or audits;
  a reused key with a different payload or actor fails closed.
- Expose payments only in authorized back-office datasets. React forms show
  eligible source orders and open invoices, retain failed input and uncertain
  retry keys, refresh after success, and support English/Traditional Chinese.
  Preserve existing journal, purchase/sales forms and full document printing.

## Tests before implementation

The first test-only iteration covers a real purchase approval/receipt followed
by invoice creation, replay/conflict, partial payment, overpayment rejection,
settlement and persistent counts. Authorization, source completion and a late
audit failure must be tested through HTTP. Expand with concurrent payment and
invoice races, receivable shipment, invalid money/date/partner and browser
flows before calling the feature stable.

No local application tests or builds run. GitHub Linux/race, browser and
Windows checks are required. Main merge, image publication and independent
OCI/Windows runtime acceptance remain separate delivery gates.
