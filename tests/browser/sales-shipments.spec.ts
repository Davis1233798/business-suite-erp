import {test,expect} from '@playwright/test';

test('purchase receipt can be sold, shipped and printed through real ERP screens',async({page})=>{
 await page.goto('/erp/login');
 await page.getByLabel('Email',{exact:true}).fill('admin@suite.test');
 await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page).toHaveURL(/\/erp\/back/);
 const purchases=page.locator('section').filter({has:page.getByRole('heading',{name:'Purchase workflow',exact:true})});
 await purchases.getByLabel('Supplier',{exact:true}).selectOption({index:1});
 const [item]=await purchases.getByLabel('Purchase item 1',{exact:true}).selectOption({index:1});
 await purchases.getByLabel('Order quantity 1',{exact:true}).fill('2');
 await purchases.getByLabel('Unit price 1',{exact:true}).fill('100');
 await purchases.getByRole('button',{name:'Create draft',exact:true}).click();
 await expect(purchases.getByRole('button',{name:'Approve order',exact:true})).toBeVisible();
 await purchases.getByRole('button',{name:'Approve order',exact:true}).click();
 const [warehouse]=await purchases.getByLabel('Warehouse',{exact:true}).selectOption({index:1});
 await purchases.getByLabel('Receipt quantity 1',{exact:true}).fill('2');
 await purchases.getByRole('button',{name:'Receive goods',exact:true}).click();
 await expect(purchases.getByText('received',{exact:true})).toBeVisible();

 const sales=page.getByRole('region',{name:'Sales workflow',exact:true});
 await sales.getByLabel('Customer',{exact:true}).selectOption({index:1});
 await sales.getByLabel('Sales item 1',{exact:true}).selectOption(item);
 await sales.getByLabel('Sales quantity 1',{exact:true}).fill('2');
 await sales.getByLabel('Sales unit price 1',{exact:true}).fill('500');
 const created=page.waitForResponse(r=>r.url().endsWith('/api/stations/erp/sales'));
 await sales.getByRole('button',{name:'Create sales draft',exact:true}).click();
 const order=(await(await created).json()).data.id;
 await expect(sales.getByLabel('Sales order',{exact:true})).toHaveValue(order);
 await sales.getByRole('button',{name:'Confirm sales order',exact:true}).click();
 await sales.getByLabel('Shipment warehouse',{exact:true}).selectOption(warehouse);
 await sales.getByLabel('Ship quantity 1',{exact:true}).fill('3');
 await sales.getByRole('button',{name:'Ship goods',exact:true}).click();
 await expect(sales.getByRole('alert')).toBeVisible();
 await expect(sales.getByLabel('Ship quantity 1',{exact:true})).toHaveValue('3');
 await sales.getByLabel('Ship quantity 1',{exact:true}).fill('2');
 const sent=page.waitForResponse(r=>r.url().endsWith('/api/stations/erp/sales'));
 await sales.getByRole('button',{name:'Ship goods',exact:true}).click();
 const shipment=(await(await sent).json()).data;
 await expect(sales.getByText('shipped',{exact:true})).toBeVisible();
 const data=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 const line=data.shipmentLines.find((row:{shipmentId:string})=>row.shipmentId===shipment.id);
 expect(line.quantity).toBe(2);expect(line.extendedCost).toBe(shipment.extendedCost);
 await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1)).toBe(true);
 await page.goto('/erp/print');
 await page.getByLabel('Print format',{exact:true}).selectOption('erp-shipment');
 await page.getByLabel('Document or product',{exact:true}).selectOption(shipment.id);
 await page.getByRole('button',{name:'Generate preview',exact:true}).click();
 await expect(page.locator('.print-sheet')).toContainText(shipment.shipmentNumber);
});

test('read-only role has no sales mutation region',async({page})=>{
 await page.goto('/erp/login');await page.getByLabel('Email',{exact:true}).fill('viewer@suite.test');
 await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page).toHaveURL(/\/erp\/back/);
 await expect(page.getByRole('region',{name:'Sales workflow',exact:true})).toHaveCount(0);
});
