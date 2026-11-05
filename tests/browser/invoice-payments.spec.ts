import {test,expect} from '@playwright/test';

test('invoice and partial/full payment persist through the real ERP interface',async({page})=>{
 await page.goto('/erp/login');
 await page.getByLabel('Email',{exact:true}).fill('admin@suite.test');
 await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page).toHaveURL(/\/erp\/back/);
 const initial=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 const send=async(payload:Record<string,unknown>)=>{
  const response=await page.request.post('/api/stations/erp/purchases',{data:{...payload,requestId:crypto.randomUUID()}});
  expect(response.ok()).toBe(true);return (await response.json()).data;
 };
 const purchase=await send({action:'create',supplierId:initial.suppliers[0].id,lines:[{itemId:initial.items[0].id,quantity:3,unitPrice:120}]});
 await send({action:'approve',poId:purchase.id});
 const created=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 const line=created.purchaseLines.find((row:{poId:string})=>row.poId===purchase.id);
 await send({action:'receive',poId:purchase.id,warehouseId:initial.warehouses[0].id,lines:[{lineId:line.id,quantity:3}]});
 await page.reload();
 const billing=page.getByRole('region',{name:'Invoices and payments',exact:true});
 await billing.getByLabel('Completed source order',{exact:true}).selectOption(purchase.id);
 await billing.getByLabel('Due date',{exact:true}).fill('2035-01-15');
 const invoiceResponse=page.waitForResponse(response=>response.url().endsWith('/api/stations/erp/finance')&&response.request().method()==='POST');
 await billing.getByRole('button',{name:'Create internal invoice',exact:true}).click();
 const invoice=(await(await invoiceResponse).json()).data;
 expect(invoice.totalAmount).toBe(360);
 await expect(billing.getByRole('status')).toContainText('Internal invoice created');
 await billing.getByLabel('Open invoice',{exact:true}).selectOption(invoice.invoiceId);
 await billing.getByLabel('Payment amount',{exact:true}).fill('100');
 await billing.getByRole('button',{name:'Record payment',exact:true}).click();
 await expect(billing.getByRole('status')).toContainText('Payment recorded');
 await billing.getByLabel('Open invoice',{exact:true}).selectOption(invoice.invoiceId);
 await billing.getByLabel('Payment amount',{exact:true}).fill('261');
 await expect(billing.getByRole('button',{name:'Record payment',exact:true})).toBeDisabled();
 await billing.getByLabel('Payment amount',{exact:true}).fill('260');
 await billing.getByRole('button',{name:'Record payment',exact:true}).click();
 await expect(billing.getByRole('status')).toContainText('Payment recorded');
 const saved=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 expect(saved.invoices.find((row:{id:string})=>row.id===invoice.invoiceId)).toMatchObject({paidAmount:360,status:'paid'});
 expect(saved.payments.filter((row:{invoiceId:string})=>row.invoiceId===invoice.invoiceId)).toHaveLength(2);
 await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
 await page.evaluate(()=>localStorage.setItem('suite-language','zh-TW'));await page.reload();
 await expect(page.getByRole('region',{name:'應收應付與核銷',exact:true})).toBeVisible();
});

test('read-only accounts cannot see finance forms',async({page})=>{
 await page.goto('/erp/login');await page.getByLabel('Email',{exact:true}).fill('viewer@suite.test');await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page).toHaveURL(/\/erp\/back/);
 await expect(page.getByRole('region',{name:'Invoices and payments',exact:true})).toHaveCount(0);
});

test('a lost payment response retries the committed intent without a duplicate posting',async({page})=>{
 await page.goto('/erp/login');await page.getByLabel('Email',{exact:true}).fill('office@suite.test');await page.getByLabel('Password',{exact:true}).fill('SuiteDemo!2026');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page).toHaveURL(/\/erp\/back/);
 const before=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 const invoice=before.invoices.find((row:{totalAmount:number;paidAmount:number;status:string})=>row.totalAmount-row.paidAmount>=100&&(row.status==='unpaid'||row.status==='partially_paid'));
 expect(invoice).toBeTruthy();
 const intents:Record<string,unknown>[]=[];
 await page.route('**/api/stations/erp/finance',async route=>{
  intents.push(route.request().postDataJSON());
  if(intents.length===1){
   const committed=await route.fetch();expect(committed.status()).toBe(200);
   await route.abort('failed');
  }else await route.continue();
 });
 const panel=page.getByRole('region',{name:'Invoices and payments',exact:true});
 await panel.getByLabel('Open invoice',{exact:true}).selectOption(invoice.id);
 await panel.getByLabel('Payment amount',{exact:true}).fill('100');
 await panel.getByRole('button',{name:'Record payment',exact:true}).click();
 await expect(panel.getByRole('alert')).toBeVisible();
 await expect(panel.getByLabel('Open invoice',{exact:true})).toHaveValue(invoice.id);
 await expect(panel.getByLabel('Payment amount',{exact:true})).toHaveValue('100');
 await panel.getByRole('button',{name:'Record payment',exact:true}).click();
 await expect(panel.getByRole('status')).toContainText('Payment recorded');
 expect(intents).toHaveLength(2);expect(intents[1]).toEqual(intents[0]);
 const after=(await(await page.request.get('/api/stations/erp/data?view=back')).json()).data;
 expect(after.invoices.find((row:{id:string})=>row.id===invoice.id).paidAmount).toBe(invoice.paidAmount+100);
 const count=(data:{payments:{invoiceId:string}[]})=>data.payments.filter(row=>row.invoiceId===invoice.id).length;
 expect(count(after)).toBe(count(before)+1);
});
