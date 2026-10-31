import { eq } from "drizzle-orm";
import { getDb } from "../db/index";
import {
  organizations,
  demoSeedStates,
  users,
  memberships,
  erpWarehouses,
  erpItems,
  erpSuppliers,
  erpCustomers,
  erpPurchaseOrders,
  erpPurchaseOrderLines,
  erpSalesOrders,
  erpSalesOrderLines,
  erpStockLedger,
  erpItemCosting,
  erpInvoices,
  erpJournalEntries,
  erpJournalLines,
  erpFiscalPeriods,
} from "../db/schema";

// Fictional fixtures only: no real brands, personal phone numbers or delivery addresses.
export async function seedOrganization(orgId: string = "org_demo_default") {
  const db = getDb();

  // 1. Organization & Core Users
  const existingOrg = await db
    .select()
    .from(organizations)
    .where(eq(organizations.id, orgId))
    .limit(1);

  if (existingOrg.length === 0) {
    await db.insert(organizations).values({
      id: orgId,
      name: "示範企業 (Business Suite Enterprise)",
      slug: orgId,
    }).onConflictDoNothing();
  }

  // Users
  const adminUser = { id: `usr_${orgId}_admin`, email: "admin@suite.test", displayName: "示範管理員 (Admin)" };
  const operatorUser = { id: `usr_${orgId}_op`, email: "operator@suite.test", displayName: "示範作業員 / 現場主管" };
  const readonlyUser = { id: `usr_${orgId}_ro`, email: "guest@suite.test", displayName: "訪客 (唯讀)" };

  for (const u of [adminUser, operatorUser, readonlyUser]) {
    const exists = await db.select().from(users).where(eq(users.id, u.id)).limit(1);
    if (exists.length === 0) {
      await db.insert(users).values(u).onConflictDoNothing();
    }
  }

  // Memberships
  const memberRoles = [
    { userId: adminUser.id, role: "admin" },
    { userId: operatorUser.id, role: "operator" },
    { userId: readonlyUser.id, role: "readonly" },
  ];
  for (const m of memberRoles) {
    const memId = `mem_${orgId}_${m.userId}`;
    const exists = await db.select().from(memberships).where(eq(memberships.id, memId)).limit(1);
    if (exists.length === 0) {
      await db.insert(memberships).values({
        id: memId,
        organizationId: orgId,
        userId: m.userId,
        role: m.role,
      }).onConflictDoNothing();
    }
  }

  // ==========================================
  // 3. ERP「LedgerWorks」Seed
  // ==========================================
  const whMain = { id: `erp_wh_01_${orgId}`, organizationId: orgId, code: "WH-TP", name: "台北總倉", isActive: true };
  const whSec = { id: `erp_wh_02_${orgId}`, organizationId: orgId, code: "WH-TC", name: "台中轉運倉", isActive: true };
  await db.insert(erpWarehouses).values([whMain, whSec]).onConflictDoNothing();

  const itemWidget = {
    id: `erp_item_01_${orgId}`,
    organizationId: orgId,
    sku: "SKU-SENS-01",
    name: "工業級溫濕度傳感器 (單件)",
    unit: "PCS",
    boxConversionRatio: 1,
    category: "電子元件",
    isActive: true,
  };
  const itemBox = {
    id: `erp_item_02_${orgId}`,
    organizationId: orgId,
    sku: "SKU-SENS-BOX10",
    name: "工業級溫濕度傳感器 (十入盒裝)",
    unit: "BOX",
    boxConversionRatio: 10, // 1 box = 10 pcs
    category: "箱裝成品",
    isActive: true,
  };
  await db.insert(erpItems).values([itemWidget, itemBox]).onConflictDoNothing();

  const sup1 = { id: `erp_sup_01_${orgId}`, organizationId: orgId, code: "SUP-DEMO-01", name: "示範供應商 A（虛構）", contactEmail: "sales@supplier-a.example.test", isActive: true };
  const cust1 = { id: `erp_cust_01_${orgId}`, organizationId: orgId, code: "CUST-DEMO-01", name: "示範企業客戶 A（虛構）", contactEmail: "procure@customer-a.example.test", isActive: true };
  await db.insert(erpSuppliers).values(sup1).onConflictDoNothing();
  await db.insert(erpCustomers).values(cust1).onConflictDoNothing();

  // Costing & stock ledger: 10 @ 100 then 10 @ 200 = 20 @ 150
  await db.insert(erpItemCosting).values({
    id: `erp_cost_01_${orgId}`,
    organizationId: orgId,
    itemId: itemWidget.id,
    totalQuantity: 20,
    totalCost: 3000,
    movingAverageCost: 150,
  }).onConflictDoNothing();

  await db.insert(erpStockLedger).values([
    {
      id: `erp_stk_01_${orgId}`,
      organizationId: orgId,
      itemId: itemWidget.id,
      warehouseId: whMain.id,
      changeType: "purchase_receipt",
      quantityChange: 10,
      balanceAfter: 10,
      unitCost: 100,
      totalValue: 1000,
      referenceDoc: "PO-202610-001",
    },
    {
      id: `erp_stk_02_${orgId}`,
      organizationId: orgId,
      itemId: itemWidget.id,
      warehouseId: whMain.id,
      changeType: "purchase_receipt",
      quantityChange: 10,
      balanceAfter: 20,
      unitCost: 200,
      totalValue: 3000,
      referenceDoc: "PO-202610-002",
    },
  ]).onConflictDoNothing();

  // PO & SO
  const po1 = {
    id: `erp_po_01_${orgId}`,
    organizationId: orgId,
    poNumber: "PO-202610-001",
    supplierId: sup1.id,
    status: "approved",
    totalAmount: 1000, // 10 * 100
  };
  await db.insert(erpPurchaseOrders).values(po1).onConflictDoNothing();
  await db.insert(erpPurchaseOrderLines).values({
    id: `erp_pol_01_${orgId}`,
    organizationId: orgId,
    poId: po1.id,
    itemId: itemWidget.id,
    quantity: 10,
    unitPrice: 100,
    receivedQuantity: 10,
  }).onConflictDoNothing();

  const so1 = {
    id: `erp_so_01_${orgId}`,
    organizationId: orgId,
    soNumber: "SO-202610-001",
    customerId: cust1.id,
    quoteReference: "QT-2026-001",
    status: "confirmed",
    totalAmount: 1500, // 5 * 300
  };
  await db.insert(erpSalesOrders).values(so1).onConflictDoNothing();
  await db.insert(erpSalesOrderLines).values({
    id: `erp_sol_01_${orgId}`,
    organizationId: orgId,
    soId: so1.id,
    itemId: itemWidget.id,
    quantity: 5,
    unitPrice: 300,
    shippedQuantity: 0,
  }).onConflictDoNothing();

  // Fiscal period
  await db.insert(erpFiscalPeriods).values({
    id: `erp_fp_202610_${orgId}`,
    organizationId: orgId,
    periodCode: "2026-10",
    startDate: "2026-10-01",
    endDate: "2026-10-31",
    isClosed: false,
  }).onConflictDoNothing();

  // Invoice with partial payment: 1000, paid 400, unpaid 600
  await db.insert(erpInvoices).values({
    id: `erp_inv_01_${orgId}`,
    organizationId: orgId,
    invoiceNumber: "INV-202610-001",
    invoiceType: "payable",
    partnerType: "supplier",
    partnerId: sup1.id,
    totalAmount: 1000,
    paidAmount: 400,
    status: "partially_paid",
    dueDate: "2026-11-15",
  }).onConflictDoNothing();

  // Balanced Journal Entry
  const je1 = {
    id: `erp_je_01_${orgId}`,
    organizationId: orgId,
    entryNumber: "JE-202610-001",
    entryDate: "2026-10-15",
    description: "期初股本與銀行存款開帳",
    referenceDoc: "INITIAL_CAPITAL",
    status: "posted",
  };
  await db.insert(erpJournalEntries).values(je1).onConflictDoNothing();
  await db.insert(erpJournalLines).values([
    {
      id: `erp_jel_01_${orgId}`,
      organizationId: orgId,
      journalId: je1.id,
      accountCode: "1101",
      accountName: "銀行存款 (Bank Deposit)",
      debitAmount: 100000,
      creditAmount: 0,
    },
    {
      id: `erp_jel_02_${orgId}`,
      organizationId: orgId,
      journalId: je1.id,
      accountCode: "3101",
      accountName: "普通股股本 (Common Stock)",
      debitAmount: 0,
      creditAmount: 100000,
    },
  ]).onConflictDoNothing();

  await db.insert(demoSeedStates).values({ organizationId: orgId }).onConflictDoNothing();
  return { success: true, organizationId: orgId };
}
