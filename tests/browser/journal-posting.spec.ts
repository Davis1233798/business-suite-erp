import {test,expect} from '@playwright/test';
async function signIn(page:import('@playwright/test').Page,email='office@suite.test'){
 await page.goto('/erp/login');await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page).toHaveURL(/\/erp\/back/);
}
test('operator posts a persisted balanced journal through the form',async({page})=>{
 await signIn(page);const panel=page.locator('section').filter({has:page.getByRole('heading',{name:'Post a journal entry'})});
 await panel.getByLabel('Entry date').fill('2027-03-04');await panel.getByLabel('Description').fill('Browser journal acceptance');
 await panel.getByLabel('Account code 1',{exact:true}).fill('1100');await panel.getByLabel('Account code 2',{exact:true}).fill('4100');await panel.getByLabel('Account name',{exact:true}).nth(0).fill('Cash');await panel.getByLabel('Account name',{exact:true}).nth(1).fill('Revenue');await panel.getByLabel('Debit',{exact:true}).nth(0).fill('1200');await panel.getByLabel('Credit',{exact:true}).nth(1).fill('1200');
 await panel.getByRole('button',{name:'Post journal',exact:true}).click();await expect(panel.getByRole('status')).toContainText('Posted JE-');
 const data=await (await page.request.get('/api/stations/erp/data?view=back')).json();const journal=data.data.journalEntries.find((r:{description:string})=>r.description==='Browser journal acceptance');expect(journal).toBeTruthy();const lines=data.data.journalLines.filter((r:{journalId:string})=>r.journalId===journal.id);expect(lines).toHaveLength(2);expect(lines.reduce((n:number,r:{debitAmount:number})=>n+r.debitAmount,0)).toBe(1200);
 await page.reload();await expect(page.getByText('Browser journal acceptance',{exact:true})).toBeVisible();
});
test('server rejection retains entered lines and description',async({page})=>{
 await signIn(page);const panel=page.locator('section').filter({has:page.getByRole('heading',{name:'Post a journal entry'})});await panel.getByLabel('Entry date').fill('2027-03-04');await panel.getByLabel('Description').fill('Keep rejected draft');
 for(let i=0;i<2;i++){await panel.getByLabel('Account code '+(i+1),{exact:true}).fill('1100');await panel.getByLabel('Account name',{exact:true}).nth(i).fill('Account')}
 await panel.getByLabel('Debit',{exact:true}).nth(0).fill('1200');await panel.getByLabel('Credit',{exact:true}).nth(0).fill('100');await panel.getByLabel('Credit',{exact:true}).nth(1).fill('1100');await panel.getByRole('button',{name:'Post journal',exact:true}).click();await expect(panel.getByRole('alert')).toBeVisible();await expect(panel.getByLabel('Description')).toHaveValue('Keep rejected draft');await expect(panel.getByLabel('Debit',{exact:true}).nth(0)).toHaveValue('1200');
});
test('read-only user has no posting form',async({page})=>{await signIn(page,'viewer@suite.test');await expect(page.getByRole('heading',{name:'Post a journal entry'})).toHaveCount(0)});
test('operator closes a fiscal period and sees the saved state after reload', async ({page}) => {
  await signIn(page);
  const panel = page.locator('section').filter({has: page.getByRole('heading', {name: 'Fiscal periods'})});
  const period = panel.getByRole('listitem').filter({hasText: '2026-10'});
  await expect(period).toContainText('Open');
  await period.getByRole('button', {name: 'Close period', exact: true}).click();
  await panel.getByRole('button', {name: 'Cancel', exact: true}).click();
  await expect(period).toContainText('Open');
  await period.getByRole('button', {name: 'Close period', exact: true}).click();
  await panel.getByRole('button', {name: 'Confirm close', exact: true}).click();
  await expect(period).toContainText('Closed');
  await page.reload();
  await expect(period).toContainText('Closed');
  await expect(period.getByRole('button', {name: 'Close period', exact: true})).toHaveCount(0);
});
