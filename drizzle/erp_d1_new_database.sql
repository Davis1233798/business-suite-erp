CREATE TABLE `audit_events` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`actor_id` text NOT NULL,
	`actor_role` text NOT NULL,
	`action` text NOT NULL,
	`entity_type` text NOT NULL,
	`entity_id` text NOT NULL,
	`before_value` text,
	`after_value` text,
	`reason` text,
	`request_id` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_customers` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`code` text NOT NULL,
	`name` text NOT NULL,
	`contact_email` text,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_fiscal_periods` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`period_code` text NOT NULL,
	`start_date` text NOT NULL,
	`end_date` text NOT NULL,
	`is_closed` integer DEFAULT false NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_invoices` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`invoice_number` text NOT NULL,
	`invoice_type` text NOT NULL,
	`partner_type` text NOT NULL,
	`partner_id` text NOT NULL,
	`total_amount` integer NOT NULL,
	`paid_amount` integer DEFAULT 0 NOT NULL,
	`status` text DEFAULT 'unpaid' NOT NULL,
	`due_date` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_item_costing` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`item_id` text NOT NULL,
	`total_quantity` integer DEFAULT 0 NOT NULL,
	`total_cost` integer DEFAULT 0 NOT NULL,
	`moving_average_cost` integer DEFAULT 0 NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_items` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`sku` text NOT NULL,
	`name` text NOT NULL,
	`unit` text DEFAULT 'PCS' NOT NULL,
	`box_conversion_ratio` integer DEFAULT 1 NOT NULL,
	`category` text DEFAULT 'general' NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_journal_entries` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`entry_number` text NOT NULL,
	`entry_date` text NOT NULL,
	`description` text NOT NULL,
	`reference_doc` text,
	`status` text DEFAULT 'posted' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_journal_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`journal_id` text NOT NULL,
	`account_code` text NOT NULL,
	`account_name` text NOT NULL,
	`debit_amount` integer DEFAULT 0 NOT NULL,
	`credit_amount` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_payments` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`payment_number` text NOT NULL,
	`invoice_id` text NOT NULL,
	`amount` integer NOT NULL,
	`payment_method` text DEFAULT 'bank_transfer' NOT NULL,
	`notes` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_purchase_order_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`po_id` text NOT NULL,
	`item_id` text NOT NULL,
	`quantity` integer NOT NULL,
	`unit_price` integer NOT NULL,
	`received_quantity` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_purchase_orders` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`po_number` text NOT NULL,
	`supplier_id` text NOT NULL,
	`status` text DEFAULT 'draft' NOT NULL,
	`total_amount` integer DEFAULT 0 NOT NULL,
	`rejected_reason` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_receipt_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`receipt_id` text NOT NULL,
	`item_id` text NOT NULL,
	`quantity` integer NOT NULL,
	`unit_cost` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_receipts` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`receipt_number` text NOT NULL,
	`po_id` text NOT NULL,
	`warehouse_id` text NOT NULL,
	`status` text DEFAULT 'completed' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_sales_order_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`so_id` text NOT NULL,
	`item_id` text NOT NULL,
	`quantity` integer NOT NULL,
	`unit_price` integer NOT NULL,
	`shipped_quantity` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_sales_orders` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`so_number` text NOT NULL,
	`customer_id` text NOT NULL,
	`quote_reference` text,
	`status` text DEFAULT 'draft' NOT NULL,
	`total_amount` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_shipment_lines` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`shipment_id` text NOT NULL,
	`item_id` text NOT NULL,
	`quantity` integer NOT NULL,
	`unit_cost` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_shipments` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`shipment_number` text NOT NULL,
	`so_id` text NOT NULL,
	`warehouse_id` text NOT NULL,
	`status` text DEFAULT 'completed' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_stock_ledger` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`item_id` text NOT NULL,
	`warehouse_id` text NOT NULL,
	`change_type` text NOT NULL,
	`quantity_change` integer NOT NULL,
	`balance_after` integer NOT NULL,
	`unit_cost` integer DEFAULT 0 NOT NULL,
	`total_value` integer DEFAULT 0 NOT NULL,
	`reference_doc` text NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_suppliers` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`code` text NOT NULL,
	`name` text NOT NULL,
	`contact_email` text,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `erp_warehouses` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`code` text NOT NULL,
	`name` text NOT NULL,
	`is_active` integer DEFAULT true NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `idempotency_keys` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`key` text NOT NULL,
	`endpoint` text NOT NULL,
	`request_hash` text NOT NULL,
	`response_status` integer NOT NULL,
	`response_body` text NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `memberships` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`user_id` text NOT NULL,
	`role` text DEFAULT 'operator' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `organizations` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`slug` text NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `organizations_slug_unique` ON `organizations` (`slug`);
--> statement-breakpoint
CREATE TABLE `outbox_events` (
	`id` text PRIMARY KEY NOT NULL,
	`organization_id` text NOT NULL,
	`event_type` text NOT NULL,
	`payload` text NOT NULL,
	`status` text DEFAULT 'pending' NOT NULL,
	`retry_count` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
--> statement-breakpoint
CREATE TABLE `users` (
	`id` text PRIMARY KEY NOT NULL,
	`email` text NOT NULL,
	`display_name` text NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idempotency_scope_unique ON idempotency_keys(organization_id, key, endpoint);
--> statement-breakpoint
DROP TRIGGER IF EXISTS erp_stock_ledger_consistency;
--> statement-breakpoint
CREATE TRIGGER IF NOT EXISTS erp_stock_ledger_consistency BEFORE INSERT ON erp_stock_ledger WHEN NOT EXISTS (SELECT 1 FROM erp_stock_ledger WHERE id=NEW.id) AND (NEW.balance_after < 0 OR NEW.balance_after != COALESCE((SELECT balance_after FROM erp_stock_ledger WHERE organization_id=NEW.organization_id AND item_id=NEW.item_id AND warehouse_id=NEW.warehouse_id ORDER BY rowid DESC LIMIT 1),0) + NEW.quantity_change) BEGIN SELECT RAISE(ABORT, 'Stock changed concurrently; retry operation'); END;
--> statement-breakpoint
CREATE TRIGGER IF NOT EXISTS erp_invoice_overpayment BEFORE UPDATE OF paid_amount ON erp_invoices WHEN NEW.paid_amount < 0 OR NEW.paid_amount > NEW.total_amount BEGIN SELECT RAISE(ABORT, 'Invoice overpayment'); END;
--> statement-breakpoint
CREATE TRIGGER IF NOT EXISTS erp_shipment_limit BEFORE UPDATE OF shipped_quantity ON erp_sales_order_lines WHEN NEW.shipped_quantity < 0 OR NEW.shipped_quantity > NEW.quantity BEGIN SELECT RAISE(ABORT, 'Shipment exceeds ordered quantity'); END;
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS demo_seed_states (organization_id TEXT PRIMARY KEY NOT NULL, ready_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
--> statement-breakpoint
CREATE TRIGGER IF NOT EXISTS erp_journal_period_closed BEFORE INSERT ON erp_journal_entries WHEN EXISTS (SELECT 1 FROM erp_fiscal_periods WHERE organization_id=NEW.organization_id AND is_closed=1 AND start_date<=NEW.entry_date AND end_date>=NEW.entry_date) BEGIN SELECT RAISE(ABORT, 'Fiscal period is closed'); END;

CREATE TABLE erp_vinext_schema(version INTEGER NOT NULL); INSERT INTO erp_vinext_schema VALUES (1);
