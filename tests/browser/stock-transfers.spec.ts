import {test,expect} from '@playwright/test';

async function signIn(page:import('@playwright/test').Page,email='office@suite.test'){
 await page.goto('/erp/login');await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page).toHaveURL(/\/erp\/back/);
}

test('operator transfers stock from persisted availability and sees it after reload',async({page})=>{
 await signIn(page);const panel=page.locator('section').filter({has:page.getByRole('heading',{name:'Warehouse transfer',exact:true})});
 const source=panel.getByLabel('Source warehouse',{exact:true}),destination=panel.getByLabel('Destination warehouse',{exact:true});
 const sourceOptions=await source.locator('option').evaluateAll(options=>options.map(o=>(o as HTMLOptionElement).value));
 const destinationOptions=await destination.locator('option').evaluateAll(options=>options.map(o=>(o as HTMLOptionElement).value));
 await source.selectOption(sourceOptions[0]);await destination.selectOption(destinationOptions.find(x=>x!==sourceOptions[0])!);
 await expect(panel.getByText(/Available:/)).toBeVisible();
 const input=panel.getByLabel('Transfer quantity',{exact:true});await input.fill('1');await panel.getByRole('button',{name:'Confirm stock transfer',exact:true}).click();
 await expect(panel.getByRole('status')).toContainText('Transfer complete');
 const result=await(await page.request.get('/api/stations/erp/data?view=back')).json();expect(result.data.stockLedger.some((r:{changeType:string})=>r.changeType==='transfer_out')).toBeTruthy();
 await page.reload();await expect(page.getByRole('heading',{name:'Warehouse transfer',exact:true})).toBeVisible();
});

test('read-only and front-office accounts have no transfer form',async({page})=>{
 await signIn(page,'viewer@suite.test');await expect(page.getByRole('heading',{name:'Warehouse transfer',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Sign out'}).click();await signIn(page,'front@suite.test');await expect(page.getByRole('heading',{name:'Warehouse transfer',exact:true})).toHaveCount(0);
});
