import { test, expect } from '@playwright/test';

test('administrator approves a physical count in the Traditional Chinese mobile workspace', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => localStorage.setItem('suite-language', 'zh-TW'));
  await page.goto('/erp/login');
  await page.getByLabel('電子郵件', { exact: true }).fill('admin@suite.test');
  await page.getByLabel('密碼', { exact: true }).fill('SuiteDemo!2026');
  await page.getByRole('button', { name: '登入', exact: true }).click();
  await expect(page).toHaveURL(/\/erp\/back$/);

  const response = await page.request.get('/api/stations/erp/data?view=back');
  expect(response.ok()).toBe(true);
  const { data } = await response.json();
  const seeded = data.stockLedger.find((row: { balanceAfter: number }) => row.balanceAfter === 20);
  expect(seeded).toBeTruthy();

  const count = page.getByRole('region', { name: '實體盤點' });
  await count.getByLabel('盤點品項', { exact: true }).selectOption(seeded.itemId);
  await count.getByLabel('盤點倉庫', { exact: true }).selectOption(seeded.warehouseId);
  await expect(count.getByText('目前庫存：20', { exact: true })).toBeVisible();
  await count.getByLabel('實盤數量', { exact: true }).fill('19');
  await count.getByLabel('盤點原因', { exact: true }).fill('現場少一件');
  const committed = page.waitForResponse(r => r.url().endsWith('/api/stations/erp/stock-count') && r.request().method() === 'POST');
  await count.getByRole('button', { name: '核准盤點', exact: true }).click();
  const adjustment = await committed;
  expect(adjustment.ok()).toBe(true);
  const result = (await adjustment.json()).data;
  expect(result).toMatchObject({ balanceAfter: 19, delta: -1, valueChange: -150 });
  await expect(count.getByRole('status')).toContainText(result.referenceDoc);
  await expect(count.getByText('目前庫存：19', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);

  await page.reload();
  const updated = (await (await page.request.get('/api/stations/erp/data?view=back')).json()).data;
  expect(updated.stockLedger.find((row: { referenceDoc: string }) => row.referenceDoc === result.referenceDoc)).toMatchObject({
    changeType: 'adjustment', quantityChange: -1, balanceAfter: 19,
  });
});
