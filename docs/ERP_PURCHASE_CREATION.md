# Standalone ERP purchase creation

The purchasing back office submits an order with a supplier and item lines from
the signed-in organization. The API calculates the total and records the order,
lines, retry result, and audit event in one transaction. Existing approval,
receiving, and return operations continue to own their respective transitions.

GitHub CI validates creation, tenant boundaries, role enforcement, and replay.
