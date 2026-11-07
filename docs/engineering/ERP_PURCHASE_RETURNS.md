# ERP purchase returns

Port supplier stock returns from the portfolio ERP demo to the independent
ERP application. A return references a completed receipt line and removes
stock from its original receiving warehouse.

## Program boundaries

- Back-office administrators and operators can read the remaining return
  quantity and submit a positive integer amount with a reason. Resolve the
  warehouse and item from the selected tenant's completed receipt line.
- Limit the transaction to the received quantity minus prior returns and
  the current stock at the receipt warehouse. Check global ledger quantity
  against item costing in the same transaction.
- Allocate value proportionally from the current moving-average inventory
  cost, retaining any rounding remainder for later issues. The receipt
  remains a historical record. Supplier credit and payable adjustments
  are separate financial operations.
- Write the ledger, exact costing, audit reason and durable retry result
  atomically. Scope the retry key to the authenticated actor and endpoint.
  Preserve the original request after transport or gateway uncertainty.
- Add the bilingual back-office form with remaining receipt quantity,
  warehouse availability and the saved return reference.

## Remote acceptance

Create and receive a purchase through the real ERP API. Exercise partial and
full return, tenant and role denial, concurrent requests, exact costing and
late audit rollback. Verify that a retry changes neither stock nor audit
history. Browser acceptance checks persisted availability and reload.
