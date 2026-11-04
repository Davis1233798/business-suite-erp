# ERP journal posting vertical flow

## Architecture and program boundaries

Implement real Go ledger posting at POST /api/stations/erp/journal, never proxy to the legacy TypeScript API. Resolve the organization and current ERP grant from the server session; only back-office operator/admin may post. Browser organization/user/role fields cannot select a tenant. Posted entries are immutable through this endpoint.

Use integer minor currency units, 2-100 bounded lines, real YYYY-MM-DD dates, nonempty bounded descriptions/account fields, and exactly one positive debit or credit per line. Each amount is at most 10,000,000. Total debits must equal total credits and be positive. Reject closed fiscal periods inside the same SQLite transaction as all header/line writes. Any insertion failure rolls back the entire journal. Generate cryptographically unique IDs and a unique entry number. No partially posted header may remain.

The React back-office journal form must show debit/credit totals, validate and submit the actual API, refresh journal datasets after success, and retain input with a visible error on rejection. Front-office/read-only users have no posting control; server checks remain authoritative. Printing and existing dataset queries remain supported. This vertical flow does not claim purchases, receipts, inventory or all ERP migration is complete.

## Tests before code

Remote Go HTTP integration tests cover actual admin/operator login, successful persisted balanced entry, cross-organization isolation, anonymous/read-only/front denial, invalid dates, negative/fractional/zero/both-sided/imbalanced amounts, bounded line count, closed-period rejection and transaction rollback on a forced late line failure. Tests run in GitHub CI only; no local app tests/builds.

## Release acceptance

Remote Linux race tests, TypeScript/build and Windows packaging must pass on the exact PR head before autonomous review and main merge. OCI deployment and real Windows install/client-server acceptance are separate evidence; neither is implied by a green unit test.