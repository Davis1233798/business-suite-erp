import { expect, test, type Page } from '@playwright/test';

const transferRegion = (page: Page) => page.locator('section[aria-label="Warehouse transfer"]');

async function signIn(page: Page, email = 'office@suite.test') {
  const demoSpace = crypto.randomUUID().replaceAll('-', '').padEnd(64, '0').slice(0, 64);
  await page.context().addCookies([
    { name: 'suite_demo_space', value: demoSpace, domain: '127.0.0.1', path: '/' },
  ]);
  await page.goto('/erp/login');
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill('SuiteDemo!2026');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/erp\/(back|front)/);
  if (email === 'office@suite.test' || email === 'admin@suite.test') {
    await page.goto('/erp/back');
  }
}

async function chooseStockedWarehouses(page: Page) {
  const panel = transferRegion(page);
  await expect(panel.locator('select')).toHaveCount(3);
  const warehouseData = await (await page.request.get('/api/stations/erp/data?view=back')).json();
  const itemId = await panel.locator('select').nth(0).inputValue();
  const warehouses = warehouseData.data.warehouses as Array<{ id: string }>;
  const balances = await Promise.all(warehouses.map(async warehouse => {
    const response = await page.request.get(
      `/api/stations/erp/inventory?itemId=${encodeURIComponent(itemId)}&warehouseId=${encodeURIComponent(warehouse.id)}`,
    );
    const result = await response.json();
    return { id: warehouse.id, available: result.data.available as number };
  }));
  const sourceWarehouse = balances.find(warehouse => warehouse.available > 0);
  const destinationWarehouse = warehouses.find(warehouse => warehouse.id !== sourceWarehouse?.id);
  expect(sourceWarehouse, 'fixture must provide at least one stocked warehouse').toBeTruthy();
  expect(destinationWarehouse, 'fixture must provide a distinct destination warehouse').toBeTruthy();
  await panel.locator('select').nth(1).selectOption(sourceWarehouse!.id);
  await panel.locator('select').nth(2).selectOption(destinationWarehouse!.id);
  await expect(panel.getByText(/Available:/)).toHaveText(`Available: ${sourceWarehouse!.available}`);
  return { panel, sourceWarehouse: sourceWarehouse!, destinationWarehouse: destinationWarehouse! };
}

async function receiveStockThroughPurchaseWorkflow(page: Page) {
  const panel = page.locator('section').filter({
    has: page.getByRole('heading', { name: 'Purchase workflow', exact: true }),
  });
  await panel.getByLabel('Supplier', { exact: true }).selectOption({ index: 1 });
  await panel.getByLabel('Purchase item 1', { exact: true }).selectOption({ index: 1 });
  await panel.getByLabel('Order quantity 1', { exact: true }).fill('3');
  await panel.getByLabel('Unit price 1', { exact: true }).fill('120');

  const orderCreated = page.waitForResponse(response =>
    response.url().endsWith('/api/stations/erp/purchases') && response.request().method() === 'POST',
  );
  await panel.getByRole('button', { name: 'Create draft', exact: true }).click();
  const orderResult = await (await orderCreated).json();
  expect(orderResult.success).toBeTruthy();
  const purchaseOrderId = orderResult.data.id as string;

  await expect(panel.getByLabel('Purchase order', { exact: true })).toHaveValue(purchaseOrderId);
  await panel.getByRole('button', { name: 'Approve order', exact: true }).click();
  await panel.getByLabel('Warehouse', { exact: true }).selectOption({ index: 1 });
  await panel.getByLabel('Receipt quantity 1', { exact: true }).fill('3');
  const receiptCreated = page.waitForResponse(response =>
    response.url().endsWith('/api/stations/erp/purchases') && response.request().method() === 'POST',
  );
  await panel.getByRole('button', { name: 'Receive goods', exact: true }).click();
  const receiptResult = await (await receiptCreated).json();
  expect(receiptResult.success).toBeTruthy();
  await expect(panel.getByText('received', { exact: true })).toBeVisible();
}

test('administrator transfers stock from persisted availability and sees it after reload', async ({ page }) => {
  await signIn(page, 'admin@suite.test');
  await receiveStockThroughPurchaseWorkflow(page);
  const { panel, sourceWarehouse } = await chooseStockedWarehouses(page);
  const availableText = panel.getByText(/Available:/);
  await expect(availableText).toHaveText(`Available: ${sourceWarehouse.available}`);

  await panel.locator('input[type="number"]').fill('1');
  await panel.getByRole('button', { name: 'Confirm stock transfer', exact: true }).click();
  await expect(panel.getByRole('status')).toContainText('Transfer complete');
  await expect(availableText).toHaveText(`Available: ${sourceWarehouse.available - 1}`);

  const ledger = await (await page.request.get('/api/stations/erp/data?view=back')).json();
  expect(ledger.data.stockLedger.some((row: { changeType: string }) => row.changeType === 'transfer_out')).toBeTruthy();
  await page.reload();
  await expect(transferRegion(page).getByRole('heading', { name: 'Warehouse transfer', exact: true })).toBeVisible();
});

test('uncertain transfer retries preserve the original payload and key', async ({ page }) => {
  await signIn(page, 'admin@suite.test');
  const { panel } = await chooseStockedWarehouses(page);
  await panel.locator('input[type="number"]').fill('1');
  const before = await (await page.request.get('/api/stations/erp/data?view=back')).json();
  const existingLedgerIds = new Set((before.data.stockLedger as Array<{ id: string }>).map(row => row.id));

  let rejectOnce = true;
  let firstPayload: Record<string, unknown> | undefined;
  await page.route('**/api/stations/erp/inventory', async route => {
    if (route.request().method() === 'POST' && rejectOnce) {
      rejectOnce = false;
      firstPayload = route.request().postDataJSON();
      await route.abort('connectionreset');
      return;
    }
    if (route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual(firstPayload);
    }
    await route.continue();
  });

  await panel.getByRole('button', { name: 'Confirm stock transfer', exact: true }).click();
  await expect(panel.getByRole('alert')).toBeVisible();
  const retryKey = await panel.locator('code').textContent();
  expect(retryKey).toBeTruthy();
  await expect(panel.locator('select').first()).toBeDisabled();
  await expect(panel.getByRole('button', { name: 'Retry transfer', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Retry transfer', exact: true }).click();
  await expect(panel.getByRole('status')).toContainText('Transfer complete');

  const ledger = await (await page.request.get('/api/stations/erp/data?view=back')).json();
  const transferRows = (ledger.data.stockLedger as Array<{
    id: string;
    changeType: string;
    quantityChange: number;
    referenceDoc: string;
  }>).filter(row => !existingLedgerIds.has(row.id) && row.changeType.startsWith('transfer_'));
  expect(firstPayload?.requestId).toBeTruthy();
  expect(transferRows).toHaveLength(2);
  expect(transferRows[0].referenceDoc).toBe(transferRows[1].referenceDoc);
  expect(transferRows[0].quantityChange + transferRows[1].quantityChange).toBe(0);
});

test('read-only and front-office accounts have no transfer form', async ({ page }) => {
  await signIn(page, 'viewer@suite.test');
  await expect(page.getByRole('heading', { name: 'Warehouse transfer', exact: true })).toHaveCount(0);
  await page.context().clearCookies();
  await signIn(page, 'front@suite.test');
  await expect(page.getByRole('heading', { name: 'Warehouse transfer', exact: true })).toHaveCount(0);
});
