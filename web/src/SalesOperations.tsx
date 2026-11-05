import {useRef,useState} from 'react';
import {z} from 'zod';
import {currentLanguage} from './locale';

type Row=Record<string,any>;
type Line={itemId:string;quantity:number;unitPrice:number};
export function SalesOperations({data,onChanged}:{data:Record<string,Row[]>;onChanged:()=>Promise<void>}){
 const t=(en:string,zh:string)=>currentLanguage()==='zh-TW'?zh:en;
 const [customer,setCustomer]=useState(''),[lines,setLines]=useState<Line[]>([{itemId:'',quantity:1,unitPrice:0}]);
 const [selected,setSelected]=useState(''),[warehouse,setWarehouse]=useState(''),[quantities,setQuantities]=useState<Record<string,number>>({});
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[message,setMessage]=useState('');
 const requests=useRef<Record<string,{signature:string;key:string}>>({});
 const order=(data.salesOrders||[]).find(row=>row.id===selected);
 const orderLines=(data.salesLines||[]).filter(row=>row.soId===selected);
 const itemName=(id:string)=>(data.items||[]).find(row=>row.id===id)?.name||id;
 async function mutate(action:string,input:Record<string,unknown>){
  if(busy)return;setBusy(true);setError('');setMessage('');
  const payload={action,...input},signature=JSON.stringify(payload),previous=requests.current[action];
  const requestId=previous?.signature===signature?previous.key:crypto.randomUUID();requests.current[action]={signature,key:requestId};
  try{
   const response=await fetch('/api/stations/erp/sales',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({...payload,requestId})});
   const result=z.object({success:z.boolean(),error:z.object({message:z.string().optional()}).optional(),data:z.object({id:z.string(),status:z.string()}).optional()}).parse(await response.json());
   if(!response.ok||!result.success||!result.data)throw new Error(result.error?.message||t('Sales operation failed','銷售作業失敗'));
   delete requests.current[action];setMessage(t('Saved','已保存')+' · '+result.data.status);
   if(action==='create'){setSelected(result.data.id);setLines([{itemId:'',quantity:1,unitPrice:0}]);setQuantities({})}
   if(action==='ship')setQuantities({});
   try{await onChanged()}catch{setError(t('Saved. Refresh the page to load the latest data.','已保存，請重新整理頁面以取得最新資料。'))}
  }catch(e){setError(e instanceof Error?e.message:t('Sales operation failed','銷售作業失敗'))}finally{setBusy(false)}
 }
 return <section className="panel no-print" aria-label={t('Sales workflow','銷售與出貨')}><h2>{t('Sales workflow','銷售與出貨')}</h2>
  <p>{t('Create and confirm a sales order, then ship available inventory. Partial shipments preserve remaining quantities and exact inventory cost. Prices use integer minor currency units.','建立並確認銷售訂單，再從可用庫存出貨。分批出貨會保留未出貨數量與精確庫存成本。價格使用整數最小貨幣單位。')}</p>
  <form onSubmit={event=>{event.preventDefault();void mutate('create',{customerId:customer,lines})}}><fieldset disabled={busy}><legend>{t('New sales order','新增銷售訂單')}</legend>
   <label>{t('Customer','客戶')}<select aria-label={t('Customer','客戶')} required value={customer} onChange={e=>setCustomer(e.target.value)}><option value="">{t('Choose customer','選擇客戶')}</option>{(data.customers||[]).filter(row=>row.isActive).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label>
   {lines.map((line,i)=><div className="form-grid" key={i}><label>{t('Sales item','銷售品項')} {i+1}<select aria-label={t('Sales item','銷售品項')+' '+(i+1)} required value={line.itemId} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,itemId:e.target.value}:row))}><option value="">{t('Choose item','選擇品項')}</option>{(data.items||[]).filter(row=>row.isActive).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label>
    <label>{t('Sales quantity','銷售數量')} {i+1}<input aria-label={t('Sales quantity','銷售數量')+' '+(i+1)} type="number" min={1} max={10000000} step={1} required value={line.quantity} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,quantity:Number(e.target.value)}:row))}/></label>
    <label>{t('Sales unit price','銷售單價')} {i+1}<input aria-label={t('Sales unit price','銷售單價')+' '+(i+1)} type="number" min={0} max={10000000} step={1} required value={line.unitPrice} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,unitPrice:Number(e.target.value)}:row))}/></label>
    {lines.length>1&&<button type="button" className="secondary" onClick={()=>setLines(rows=>rows.filter((_,n)=>n!==i))}>{t('Remove line','移除明細')} {i+1}</button>}
   </div>)}
   <button type="button" className="secondary" disabled={lines.length>=100} onClick={()=>setLines(rows=>[...rows,{itemId:'',quantity:1,unitPrice:0}])}>{t('Add sales item','新增銷售品項')}</button> <button type="submit">{t('Create sales draft','建立銷售草稿')}</button>
  </fieldset></form>
  <fieldset disabled={busy}><legend>{t('Confirmation and shipment','確認與出貨')}</legend><label>{t('Sales order','銷售訂單')}<select aria-label={t('Sales order','銷售訂單')} value={selected} onChange={e=>{setSelected(e.target.value);setQuantities({});setError('');setMessage('')}}><option value="">{t('Choose order','選擇訂單')}</option>{(data.salesOrders||[]).map(row=><option key={row.id} value={row.id}>{row.soNumber} · {row.status}</option>)}</select></label>
   {order&&<><p>{t('Status','狀態')}: <strong>{order.status}</strong></p>{order.status==='draft'&&<button type="button" onClick={()=>void mutate('confirm',{soId:selected})}>{t('Confirm sales order','確認銷售訂單')}</button>}
    {(order.status==='confirmed'||order.status==='partially_shipped')&&<form onSubmit={event=>{event.preventDefault();void mutate('ship',{soId:selected,warehouseId:warehouse,lines:orderLines.filter(row=>(quantities[row.id]||0)>0).map(row=>({lineId:row.id,quantity:quantities[row.id]}))})}}>
     <label>{t('Shipment warehouse','出貨倉庫')}<select aria-label={t('Shipment warehouse','出貨倉庫')} required value={warehouse} onChange={e=>setWarehouse(e.target.value)}><option value="">{t('Choose warehouse','選擇倉庫')}</option>{(data.warehouses||[]).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label>
     {orderLines.map((row,i)=><div className="form-grid" key={row.id}><p>{itemName(row.itemId)} · {t('Remaining quantity','未出貨數量')}: {row.quantity-row.shippedQuantity}</p><label>{t('Ship quantity','本次出貨數量')} {i+1}<input aria-label={t('Ship quantity','本次出貨數量')+' '+(i+1)} type="number" min={0} max={10000000} step={1} value={quantities[row.id]||0} onChange={e=>setQuantities(values=>({...values,[row.id]:Number(e.target.value)}))}/></label></div>)}
     <button type="submit">{t('Ship goods','確認出貨')}</button>
    </form>}
   </>}
  </fieldset>{error&&<p role="alert" className="error">{error}</p>}{message&&<p role="status">{message}</p>}
 </section>
}
