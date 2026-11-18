# ERP 會計期間關帳防竄改與庫存守恆架構設計

## 1. 概述與系統邊界 (System Boundaries)

本文件定義 Business Suite ERP (LedgerWorks) 的 P1 核心業務邊界、狀態機、資料完整性不變量與防護機制。
ERP 模組作為企業財務、庫存與計費之權威系統，必須在面對並發寫入、跨時區操作、非法出庫與未授權操作時，保持強一致性。

### 核心責任邊界：
- **會計期間 (Fiscal Periods)**：管理會計期間開關狀態。一旦關帳 (`is_closed = 1` 或 `status = 'closed'`)，嚴格禁止寫入任何傳票 (Journal Entry)、發票 (Invoice)、付款 (Payment) 或庫存調整單 (Stock Count Adjustment)。
- **庫存台帳 (Stock Ledger & Inventory)**：以不可變之流水帳記錄所有出入庫異動（`purchase_receipt`, `purchase_return`, `sales_shipment`, `transfer_in`, `transfer_out`, `adjustment`）。
- **計費與核銷 (Billing & Settlement)**：以應收應付發票及付款記錄為基礎，關聯採購與銷售訂單，確保金額不超收、不重複沖銷。
- **加權移動平均成本 (Moving Average Cost - E08)**：依進貨批次動態重算單元平均成本，出庫時依現行均價進行成本分攤與結存。

---

## 2. 租戶隔離與權限模型 (Multi-tenant Isolation & RBAC)

1. **租戶隔離 (Tenant Isolation)**：
   - 每個 SQL 查詢與變更必須強制包含 `organization_id = ?` 條件約束。
   - 租戶 A 的會計期間關帳，絕對不影響租戶 B 的憑證錄入。
   - 租戶 A 的倉庫與料號，無法被租戶 B 查詢、出貨、調撥或調整。
2. **權限矩陣 (Role-Based Access Control)**：
   - `admin` (管理者)：擁有關閉會計期間、決定採購單 (approve/reject)、庫存盤點調整等關鍵職能。
   - `operator` (經辦員)：擁有日常進銷存建立、出貨、收貨、調撥、傳票建立與一般查詢權限，**嚴格禁止執行會計期間關帳**（關帳請求必須回傳 `403 Forbidden`）。
   - `viewer` (檢視者)：僅具讀取權限，寫入動作一律回傳 `403 Forbidden`。
   - 未認證 (Anonymous)：一律回傳 `401 Unauthorized`。

---

## 3. 狀態機與業務不變量 (State Machine & Invariants)

### 3.1 會計期間狀態機與關帳防護
- 狀態轉移：`open` -> `closed`。
- 關帳前提：僅管理者 (`admin`) 且具有後台權限 (`scope = 'back'`) 可觸發。
- 冪等關帳：重複對已關帳期間發送關帳請求，不重複產生審計事件，直接回傳關帳終態 (`200 OK`)。
- 防竄改防護：
  - 凡日期落在已關帳期間的憑證，寫入時一律回傳 `409 Conflict`。
  - 受保護單據：傳票 (`erp_journal_entries`)、採購/銷售發票 (`erp_invoices`)、付款 (`erp_payments`)、盤點調整單 (`erp_stock_ledger` 中 `change_type = 'adjustment'`)。
- **跨時區邊界防護 (Cross-timezone Protection)**：
  - 會計期間邊界以台北時間 (Asia/Taipei, UTC+8) 及標準 UTC 進行雙重檢查。
  - 若傳票或單據帶有時區時間戳（例如 `2027-01-31T16:30:00Z`，其對應台北時間為 `2027-02-01 00:30:00+08:00`），只要在台北時間或 UTC 日期任一維度落於已關帳期間內，均視為期間內單據並予以拒絕 (`409 Conflict`)，防止跨時區偷渡回寫。

### 3.2 庫存負庫存防護與調撥守恆
- **嚴格禁止負庫存 (Non-negative Inventory)**：
  - 任何倉庫出庫交易（銷售出貨 `sales_shipment`、庫存調撥出庫 `transfer_out`、採購退貨 `purchase_return`、盤點負調整）在扣減前必須原子檢查當前庫存量。
  - 當庫存不足（`stock < requested_quantity`）時，立刻拒絕交易並回傳 `409 Conflict`。
  - 底層資料庫觸發器 (`erp_stock_ledger_consistency`) 強制檢驗 `balance_after >= 0` 與 `balance_after = previous_balance + quantity_change`，杜絕競爭並發引致的負庫存。
- **調撥守恆 (Transfer Conservation)**：
  - 調撥交易在同一交易事務中寫入兩筆流水帳：
    - 來源倉出庫 (`transfer_out`)：`quantity_change = -Q`，`balance_after = source_balance - Q`
    - 目的倉入庫 (`transfer_in`)：`quantity_change = +Q`，`balance_after = dest_balance + Q`
  - 守恆定理：$\sum \Delta Q = (-Q) + (+Q) = 0$。企業總庫存總量恆定不變。
  - 價值守恆：出庫價值與入庫價值完全相等，淨庫存價值無偏差。

### 3.3 加權移動平均成本計算契約 (Moving Average Cost - E08)
- 契約驗證公式：
  1. 入庫進貨重算均價：
     $$\text{NewTotalCost} = \text{CurrentTotalCost} + (\text{IncomingQty} \times \text{IncomingUnitPrice})$$
     $$\text{NewTotalQty} = \text{CurrentTotalQty} + \text{IncomingQty}$$
     $$\text{NewMovingAvgCost} = \text{round}(\text{NewTotalCost} / \text{NewTotalQty})$$
  2. 出庫成本結轉 (COGS)：
     $$\text{AllocatedCost} = \text{round}((\text{CurrentTotalCost} \times \text{ShippedQty}) / \text{CurrentTotalQty})$$
     $$\text{RemainingCost} = \text{CurrentTotalCost} - \text{AllocatedCost}$$
     $$\text{RemainingQty} = \text{CurrentTotalQty} - \text{ShippedQty}$$
- 標準算例 (E08)：
  - 批次 1：入庫 10 件 @ 100 元 $\rightarrow$ 總量 10 件，總值 1000 元，均價 100 元。
  - 批次 2：入庫 10 件 @ 200 元 $\rightarrow$ 總量 20 件，總值 3000 元，均價 150 元。
  - 出庫：出貨 4 件 $\rightarrow$ 銷貨成本為 $4 \times 150 = 600$ 元；庫存餘量 16 件，剩餘總值 2400 元，單價維持 150 元。

---

## 4. 交易邊界與回滾策略 (Transaction Boundaries & Rollback)

1. **原子事務封裝**：
   - 每個商業 API 操作均在單一 `tx` (ACID Transaction) 內完成。
   - 包括：業務前置檢查 $\rightarrow$ 單據寫入 $\rightarrow$ 台帳異動 $\rightarrow$ 成本重算 $\rightarrow$ 審計事件 (`audit_events`) 記錄 $\rightarrow$ 冪等鍵 (`idempotency_keys`) 寫入。
2. **回滾條件 (Rollback Triggers)**：
   - 任何非預期錯誤、庫存不足、關帳期間衝突、併發修改（`RowsAffected() == 0`）或審計寫入失敗，均強制執行 `tx.Rollback()`。
   - 資料庫層嚴格不留半殘寫入或不一致的孤立單據。
3. **冪等性保障 (Idempotency Contract)**：
   - 客戶端提供 `requestId` 或 `idempotencyKey`。
   - 相同金鑰與相同輸入：安全回放既有成功結果 (`200 OK`)，不產生重複異動或二次審計。
   - 相同金鑰但不同負載：嚴格拒絕並回傳 `409 Conflict`。
