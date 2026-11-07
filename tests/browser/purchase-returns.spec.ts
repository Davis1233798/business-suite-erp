import {test,expect} from '@playwright/test';

test('administrator exercises receipt return and verifies persisted availability', async ({ page }) => {
  await page.goto('/erp/login');
  await page.getByLabel('Email', { exact: true }).fill('admin@suite.test');
  await page.getByLabel('Password', { exact: true }).fill('SuiteDemo!2026');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/erp\/back/);

  // Check if a receipt line exists; if not, create, approve and receive one.
  const purchasePanel = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Purchase workflow', exact: true }) });
  await purchasePanel.getByLabel('Supplier', { exact: true }).selectOption({ index: 1 });
  await purchasePanel.getByLabel('Purchase item 1', { exact: true }).selectOption({ index: 1 });
  await purchasePanel.getByLabel('Order quantity 1', { exact: true }).fill('2');
  await purchasePanel.getByLabel('Unit price 1', { exact: true }).fill('100');

  const created = page.waitForResponse(r => r.url().endsWith('/api/stations/erp/purchases') && r.request().method() === 'POST');
  await purchasePanel.getByRole('button', { name: 'Create draft', exact: true }).click();
  const payload = await (await created).json();
  expect(payload.success).toBe(true);
  const po = payload.data.id;

  await expect(purchasePanel.getByLabel('Purchase order', { exact: true })).toHaveValue(po);
  await purchasePanel.getByRole('button', { name: 'Approve order', exact: true }).click();
  await expect(purchasePanel.getByRole('button', { name: 'Receive goods', exact: true })).toBeVisible();

  await purchasePanel.getByLabel('Warehouse', { exact: true }).selectOption({ index: 1 });
  await purchasePanel.getByLabel('Receipt quantity 1', { exact: true }).fill('2');
  await purchasePanel.getByRole('button', { name: 'Receive goods', exact: true }).click();
  await expect(purchasePanel.getByText('received', { exact: true })).toBeVisible();

  // Now locate the purchase return panel
  const returnPanel = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Supplier stock returns', exact: true }) });
  await expect(returnPanel).toBeVisible();

  // Select the receipt line
  const select = returnPanel.getByLabel('Receipt line', { exact: true });
  await expect(select).toBeVisible();
  const count = await select.locator('option').count();
  expect(count).toBeGreaterThan(0);
  await select.selectOption({ index: count - 1 });

  // Verify availability metrics
  await expect(returnPanel.getByText('Remaining returnable')).toBeVisible();

  // Submit return
  await returnPanel.getByLabel('Return quantity', { exact: true }).fill('1');
  await returnPanel.getByLabel('Return reason', { exact: true }).fill('Supplier inspection failed');
  await returnPanel.getByRole('button', { name: 'Submit return', exact: true }).click();

  await expect(returnPanel.getByRole('status')).toContainText('Purchase return saved');

  // Reload page and check persistence
  await page.reload();
  await expect(page).toHaveURL(/\/erp\/back/);
  const reloadedPanel = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Supplier stock returns', exact: true }) });
  await expect(reloadedPanel).toBeVisible();
  const reloadedSelect = reloadedPanel.getByLabel('Receipt line', { exact: true });
  await reloadedSelect.selectOption({ index: count - 1 });
  await expect(reloadedPanel.getByText('Returned quantity')).toBeVisible();
});
