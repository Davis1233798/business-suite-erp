import { NextRequest } from "next/server";
import { z } from "zod";
import { and, eq, desc } from "drizzle-orm";
import { getDb } from "@/db";
import * as s from "@/db/schema";
import { resolveTenantContext, checkPermission, type TenantContext } from "@/lib/tenant";
import { successResponse, errorResponse } from "@/lib/api-response";
import { ErpService } from "@/lib/services/erp-service";
export const dynamic = "force-dynamic";
const id = z.string().min(1).max(150), text = z.string().trim().min(1).max(250), qty = z.number().int().positive().max(1000000);
const erpSchema = z.discriminatedUnion("action", [
  z.object({action:z.literal("item"),sku:text,name:text,boxConversionRatio:qty}),
  z.object({action:z.literal("purchase"),supplierId:id,itemId:id,quantity:qty,unitPrice:qty}),
  z.object({action:z.literal("sale"),customerId:id,itemId:id,quantity:qty,unitPrice:qty}),
  z.object({action:z.literal("approve"),poId:id,decision:z.enum(["approve","reject"]),reason:text.optional()}),
  z.object({action:z.literal("receive"),lineId:id,warehouseId:id,quantity:qty}),
  z.object({action:z.literal("ship"),lineId:id,warehouseId:id,quantity:qty}),
]);
async function erp(context: TenantContext, input: unknown) {
  const body = erpSchema.parse(input), db = getDb(), org = context.organizationId;
  if (body.action === "item") {
    const [duplicate] = await db.select().from(s.erpItems).where(and(eq(s.erpItems.organizationId,org),eq(s.erpItems.sku,body.sku)));
    if (duplicate) throw new Error("SKU 已存在");
    return ErpService.createItem(context,body);
  }
  if (body.action === "approve") return ErpService.approvePurchaseOrder(context,body.poId,body.decision === "approve",body.reason);
  if (body.action === "receive" || body.action === "ship") {
    const [warehouse] = await db.select().from(s.erpWarehouses).where(and(eq(s.erpWarehouses.organizationId,org),eq(s.erpWarehouses.id,body.warehouseId)));
    if (!warehouse) throw new Error("倉庫不存在");
    if (body.action === "receive") {
      const [line] = await db.select().from(s.erpPurchaseOrderLines).where(and(eq(s.erpPurchaseOrderLines.organizationId,org),eq(s.erpPurchaseOrderLines.id,body.lineId)));
      if (!line) throw new Error("採購明細不存在");
      return ErpService.receiveGoods(context,{poId:line.poId,warehouseId:body.warehouseId,lines:[{lineId:line.id,quantity:body.quantity}]});
    }
    const [line] = await db.select().from(s.erpSalesOrderLines).where(and(eq(s.erpSalesOrderLines.organizationId,org),eq(s.erpSalesOrderLines.id,body.lineId)));
    if (!line) throw new Error("銷售明細不存在");
    return ErpService.shipSalesOrder(context,{soId:line.soId,warehouseId:body.warehouseId,shipLines:[{lineId:line.id,quantity:body.quantity}]});
  }
  const [item] = await db.select().from(s.erpItems).where(and(eq(s.erpItems.organizationId,org),eq(s.erpItems.id,body.itemId),eq(s.erpItems.isActive,true)));
  if (!item) throw new Error("商品不存在或已停用");
  const totalAmount = body.quantity * body.unitPrice;
  if (!Number.isSafeInteger(totalAmount)) throw new Error("金額超出範圍");
  const docId = crypto.randomUUID(), lineId = crypto.randomUUID();
  if (body.action === "purchase") {
    const [supplier] = await db.select().from(s.erpSuppliers).where(and(eq(s.erpSuppliers.organizationId,org),eq(s.erpSuppliers.id,body.supplierId)));
    if (!supplier) throw new Error("供應商不存在");
    const poNumber = `PO-${Date.now()}-${docId.slice(0,4)}`;
    await db.batch([db.insert(s.erpPurchaseOrders).values({id:docId,organizationId:org,poNumber,supplierId:supplier.id,totalAmount}),db.insert(s.erpPurchaseOrderLines).values({id:lineId,organizationId:org,poId:docId,itemId:item.id,quantity:body.quantity,unitPrice:body.unitPrice})]);
    return {message:`採購單 ${poNumber} 已建立，請交由管理員審核`};
  }
  const [customer] = await db.select().from(s.erpCustomers).where(and(eq(s.erpCustomers.organizationId,org),eq(s.erpCustomers.id,body.customerId)));
  if (!customer) throw new Error("客戶不存在");
  const soNumber = `SO-${Date.now()}-${docId.slice(0,4)}`;
  await db.batch([db.insert(s.erpSalesOrders).values({id:docId,organizationId:org,soNumber,customerId:customer.id,status:"confirmed",totalAmount}),db.insert(s.erpSalesOrderLines).values({id:lineId,organizationId:org,soId:docId,itemId:item.id,quantity:body.quantity,unitPrice:body.unitPrice})]);
  return {message:`銷售單 ${soNumber} 已建立`};
}
export async function POST(request: NextRequest, {params}: {params:Promise<{module:string}>}) {
  const context = resolveTenantContext(request);
  try {
    const {module} = await params;
    const permission = checkPermission(context,"operator");
    if (!permission.allowed) return errorResponse("FORBIDDEN",permission.message!,403,undefined,context.requestId);
    const body = await request.json();
    const result = module === "erp" ? await erp(context,body) : null;
    if (!result) return errorResponse("NOT_FOUND","找不到此模組",404,undefined,context.requestId);
    return successResponse(result,context.requestId);
  } catch(error) {
    const message = error instanceof z.ZodError ? error.issues.map(issue=>`${issue.path.join(".")}: ${issue.message}`).join("；") : error instanceof Error ? error.message : "操作失敗";
    return errorResponse("OPERATION_FAILED",message,message.includes("403") ? 403 : 400,undefined,context.requestId);
  }
}
