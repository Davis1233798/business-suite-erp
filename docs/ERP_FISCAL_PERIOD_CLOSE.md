# ERP fiscal period close

The standalone ERP already rejects journal postings dated in closed fiscal
periods. The period close workflow uses the same fiscal-period records and
the existing organization-scoped station identity.

## Feature boundary

List periods for the active organization. Closing is an accounting write and
requires the same authorization as journal posting. Close a specific existing
period by ID, verify its organization, and record the actor in the existing
ERP audit trail. Commit the state change and audit together. Repeated close
requests return the closed state without an additional audit event.
Period creation, reopening and year-end adjusting entries are separate
workflows.

## Acceptance

Verify tenant isolation, read-only access, authorized close, repeat close,
and audit rollback. After close, exercise the existing journal endpoint
against both the first and last day of the period and confirm posting outside
the period remains available. Verify the period list exposes the closed state.
