import { sql } from "drizzle-orm";
import { integer, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

export const stationAccounts = sqliteTable('station_accounts', {
  userId: text('user_id').primaryKey(), organizationId: text('organization_id').notNull(),
  email: text('email').notNull(), passwordHash: text('password_hash').notNull(), disabled: integer('disabled').notNull().default(0),
}, t=>[uniqueIndex('station_accounts_email').on(t.organizationId,t.email)]);
export const stationGrants = sqliteTable('station_grants', {
  userId: text('user_id').notNull(), site: text('site').notNull(), role: text('role').notNull(),
  scope: text('scope').notNull(), enabled: integer('enabled').notNull().default(1),
},t=>[uniqueIndex('station_grants_user_site').on(t.userId,t.site)]);
export const stationSessions = sqliteTable('station_sessions', {
  tokenHash:text('token_hash').primaryKey(),userId:text('user_id').notNull(),expiresAt:integer('expires_at').notNull(),
});
export const stationLoginLimits = sqliteTable('station_login_limits', {
  key:text('key').primaryKey(),attempts:integer('attempts').notNull(),resetAt:integer('reset_at').notNull(),
});

// ==========================================
// 1. Common & Tenant Tables
// ==========================================

export const organizations = sqliteTable("organizations", {
  id: text("id").primaryKey(),
  name: text("name").notNull(),
  slug: text("slug").notNull().unique(),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const users = sqliteTable("users", {
  id: text("id").primaryKey(),
  email: text("email").notNull(),
  displayName: text("display_name").notNull(),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const memberships = sqliteTable("memberships", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  userId: text("user_id").notNull(),
  role: text("role").notNull().default("operator"), // admin, operator, readonly
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const auditEvents = sqliteTable("audit_events", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  actorId: text("actor_id").notNull(),
  actorRole: text("actor_role").notNull(),
  action: text("action").notNull(), // UPDATE_AMOUNT, UPDATE_ROLE, DEDUCT_STOCK, REFUND, etc.
  entityType: text("entity_type").notNull(),
  entityId: text("entity_id").notNull(),
  beforeValue: text("before_value"),
  afterValue: text("after_value"),
  reason: text("reason"),
  requestId: text("request_id"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const idempotencyKeys = sqliteTable("idempotency_keys", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  key: text("key").notNull(),
  endpoint: text("endpoint").notNull(),
  requestHash: text("request_hash").notNull(),
  responseStatus: integer("response_status").notNull(),
  responseBody: text("response_body").notNull(),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const outboxEvents = sqliteTable("outbox_events", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  eventType: text("event_type").notNull(),
  payload: text("payload").notNull(),
  status: text("status").notNull().default("pending"), // pending, processed, failed
  retryCount: integer("retry_count").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

// ==========================================
// 2. MES「ForgeFlow」Tables
// ==========================================

export const erpItems = sqliteTable("erp_items", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  sku: text("sku").notNull(),
  name: text("name").notNull(),
  unit: text("unit").notNull().default("PCS"),
  boxConversionRatio: integer("box_conversion_ratio").notNull().default(1),
  category: text("category").notNull().default("general"),
  isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpWarehouses = sqliteTable("erp_warehouses", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  code: text("code").notNull(),
  name: text("name").notNull(),
  isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpSuppliers = sqliteTable("erp_suppliers", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  code: text("code").notNull(),
  name: text("name").notNull(),
  contactEmail: text("contact_email"),
  isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpCustomers = sqliteTable("erp_customers", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  code: text("code").notNull(),
  name: text("name").notNull(),
  contactEmail: text("contact_email"),
  isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpPurchaseOrders = sqliteTable("erp_purchase_orders", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  poNumber: text("po_number").notNull(),
  supplierId: text("supplier_id").notNull(),
  status: text("status").notNull().default("draft"), // draft, approved, rejected, fulfilled
  totalAmount: integer("total_amount").notNull().default(0), // cents
  rejectedReason: text("rejected_reason"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpPurchaseOrderLines = sqliteTable("erp_purchase_order_lines", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  poId: text("po_id").notNull(),
  itemId: text("item_id").notNull(),
  quantity: integer("quantity").notNull(),
  unitPrice: integer("unit_price").notNull(), // cents
  receivedQuantity: integer("received_quantity").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpReceipts = sqliteTable("erp_receipts", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  receiptNumber: text("receipt_number").notNull(),
  poId: text("po_id").notNull(),
  warehouseId: text("warehouse_id").notNull(),
  status: text("status").notNull().default("completed"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpReceiptLines = sqliteTable("erp_receipt_lines", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  receiptId: text("receipt_id").notNull(),
  itemId: text("item_id").notNull(),
  quantity: integer("quantity").notNull(),
  unitCost: integer("unit_cost").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpSalesOrders = sqliteTable("erp_sales_orders", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  soNumber: text("so_number").notNull(),
  customerId: text("customer_id").notNull(),
  quoteReference: text("quote_reference"),
  status: text("status").notNull().default("draft"), // draft, confirmed, partially_shipped, shipped, cancelled
  totalAmount: integer("total_amount").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpSalesOrderLines = sqliteTable("erp_sales_order_lines", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  soId: text("so_id").notNull(),
  itemId: text("item_id").notNull(),
  quantity: integer("quantity").notNull(),
  unitPrice: integer("unit_price").notNull(),
  shippedQuantity: integer("shipped_quantity").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpShipments = sqliteTable("erp_shipments", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  shipmentNumber: text("shipment_number").notNull(),
  soId: text("so_id").notNull(),
  warehouseId: text("warehouse_id").notNull(),
  status: text("status").notNull().default("completed"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpShipmentLines = sqliteTable("erp_shipment_lines", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  shipmentId: text("shipment_id").notNull(),
  itemId: text("item_id").notNull(),
  quantity: integer("quantity").notNull(),
  unitCost: integer("unit_cost").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpStockLedger = sqliteTable("erp_stock_ledger", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  itemId: text("item_id").notNull(),
  warehouseId: text("warehouse_id").notNull(),
  changeType: text("change_type").notNull(), // purchase_receipt, purchase_return, sales_shipment, transfer_in, transfer_out, adjustment
  quantityChange: integer("quantity_change").notNull(),
  balanceAfter: integer("balance_after").notNull(),
  unitCost: integer("unit_cost").notNull().default(0),
  totalValue: integer("total_value").notNull().default(0),
  referenceDoc: text("reference_doc").notNull(),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpItemCosting = sqliteTable("erp_item_costing", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  itemId: text("item_id").notNull(),
  totalQuantity: integer("total_quantity").notNull().default(0),
  totalCost: integer("total_cost").notNull().default(0),
  movingAverageCost: integer("moving_average_cost").notNull().default(0),
  updatedAt: text("updated_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpInvoices = sqliteTable("erp_invoices", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  invoiceNumber: text("invoice_number").notNull(),
  invoiceType: text("invoice_type").notNull(), // payable, receivable
  partnerType: text("partner_type").notNull(), // supplier, customer
  partnerId: text("partner_id").notNull(),
  totalAmount: integer("total_amount").notNull(),
  paidAmount: integer("paid_amount").notNull().default(0),
  status: text("status").notNull().default("unpaid"), // unpaid, partially_paid, paid
  dueDate: text("due_date"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpPayments = sqliteTable("erp_payments", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  paymentNumber: text("payment_number").notNull(),
  invoiceId: text("invoice_id").notNull(),
  amount: integer("amount").notNull(),
  paymentMethod: text("payment_method").notNull().default("bank_transfer"),
  notes: text("notes"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpJournalEntries = sqliteTable("erp_journal_entries", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  entryNumber: text("entry_number").notNull(),
  entryDate: text("entry_date").notNull(),
  description: text("description").notNull(),
  referenceDoc: text("reference_doc"),
  status: text("status").notNull().default("posted"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpJournalLines = sqliteTable("erp_journal_lines", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  journalId: text("journal_id").notNull(),
  accountCode: text("account_code").notNull(),
  accountName: text("account_name").notNull(),
  debitAmount: integer("debit_amount").notNull().default(0),
  creditAmount: integer("credit_amount").notNull().default(0),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const erpFiscalPeriods = sqliteTable("erp_fiscal_periods", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  periodCode: text("period_code").notNull(),
  startDate: text("start_date").notNull(),
  endDate: text("end_date").notNull(),
  isClosed: integer("is_closed", { mode: "boolean" }).notNull().default(false),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

// ==========================================
// 4. POS「CounterPoint」Tables
// ==========================================

export const posCatalogItems = sqliteTable("pos_catalog_items", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  barcode: text("barcode").notNull(),
  name: text("name").notNull(),
  price: integer("price").notNull(),
  stockQuantity: integer("stock_quantity").notNull().default(0),
  isActive: integer("is_active", { mode: "boolean" }).notNull().default(true),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const posSales = sqliteTable("pos_sales", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  receiptNumber: text("receipt_number").notNull(),
  storeId: text("store_id").notNull(),
  registerId: text("register_id").notNull(),
  shiftId: text("shift_id").notNull(),
  cashierId: text("cashier_id").notNull(),
  customerId: text("customer_id"),
  subtotal: integer("subtotal").notNull(),
  discountAmount: integer("discount_amount").notNull().default(0),
  taxAmount: integer("tax_amount").notNull().default(0),
  totalAmount: integer("total_amount").notNull(),
  tenderAmount: integer("tender_amount").notNull(),
  changeAmount: integer("change_amount").notNull().default(0),
  paymentMethod: text("payment_method").notNull().default("cash"),
  status: text("status").notNull().default("completed"), // completed, refunded, partially_refunded
  isReprint: integer("is_reprint", { mode: "boolean" }).notNull().default(false),
  idempotencyKey: text("idempotency_key"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const shopOrders = sqliteTable("shop_orders", {
  id: text("id").primaryKey(),
  organizationId: text("organization_id").notNull(),
  orderNumber: text("order_number").notNull(),
  userId: text("user_id"),
  customerEmail: text("customer_email").notNull(),
  shippingAddressJson: text("shipping_address_json").notNull(),
  subtotal: integer("subtotal").notNull(),
  discountAmount: integer("discount_amount").notNull().default(0),
  shippingFee: integer("shipping_fee").notNull().default(0),
  totalAmount: integer("total_amount").notNull(),
  paymentStatus: text("payment_status").notNull().default("pending"), // pending, paid, failed, refunded
  fulfillmentStatus: text("fulfillment_status").notNull().default("unfulfilled"), // unfulfilled, partially_shipped, shipped, delivered
  trackingNumber: text("tracking_number"),
  idempotencyKey: text("idempotency_key"),
  createdAt: text("created_at").notNull().default(sql`CURRENT_TIMESTAMP`),
});

export const demoSeedStates = sqliteTable("demo_seed_states", { organizationId: text("organization_id").primaryKey(), readyAt: text("ready_at").notNull().default(sql`CURRENT_TIMESTAMP`) });
