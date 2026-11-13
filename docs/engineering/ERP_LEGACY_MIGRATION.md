# Legacy ERP data migration

The Portfolio database is the source of legacy ERP records. The standalone
ERP backup command requires an existing ERP repository schema marker. A copy
of the Portfolio database must be transformed and validated before it can
serve as the initial standalone database.

## Data boundary

Inventory the source and target schemas from consistent offline snapshots.
Map organizations and business references explicitly. Include master data,
stock balances, journal entries and lines, fiscal periods, purchase and sales
documents, invoices and payments. Preserve retry records only where the new
handler can replay the original operation safely. Keep Portfolio account
secrets and unrelated station data in the original database. Establish access
to migrated organizations through the standalone ERP identity model.

## Acceptance before public routing

Compare record counts and transaction totals by organization. Reconcile
stock by item and warehouse, balances by ledger account and outstanding
amounts by invoice. Reject orphan references and unbalanced journals. Rehearse
with synthetic data in an isolated candidate, including a retry and rollback.
During the final transfer, stop legacy ERP writes, capture a final snapshot
and reconcile again before enabling writes on the new route. Retain the
original database for rollback and reconcile any writes accepted after
cutover before routing back.

## Offline command contract

Use a consistent legacy SQLite snapshot and an explicit organization ID.
Create a new standalone file with the standard ERP initializer. Keep the
source read-only throughout. Import the selected organization's business
rows in a transaction, preserving IDs and references. Publish the output
only after reconciliation succeeds. Reject an existing destination and
leave it intact. Imported organization access requires an explicit identity
mapping before cutover.

The standalone initializer reconstructs shipment extended costs when the
source schema predates that column. The migration must exercise this path
with a synthetic shipment and compare the reconstructed costs to the
source stock ledger.
