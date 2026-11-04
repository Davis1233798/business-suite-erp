import {useRef,useState} from 'react';
import {z} from 'zod';
import {currentLanguage} from './locale';
type Row=Record<string,any>;
type OrderLine={itemId:string;quantity:number;unitPrice:number};
export function PurchaseOperations({data,role,onChanged}:{data:Record<string,Row[]>;role:string;onChanged:()=>Promise<void>}){
 const t=(en:string,zh:string)=>currentLanguage()==='zh-TW'?zh:en;
 const [supplier,setSupplier]=useState(''),[lines,setLines]=useState<OrderLine[]>([{itemId:'',quantity:1,unitPrice:0}]);
 const [selected,setSelected]=useState(''),[warehouse,setWarehouse]=useState(''),[reason,setReason]=useState(''),[quantities,setQuantities]=useState<Record<string,number>>({}),[boxes,setBoxes]=useState<Record<string,boolean>>({});
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[message,setMessage]=useState('');
 const requests=useRef<Record<string,{signature:string;key:string}>>({});
 const order=(data.purchaseOrders||[]).find(row=>row.id===selected);
 const orderLines=(data.purchaseLines||[]).filter(row=>row.poId===selected);
 const itemName=(id:string)=>(data.items||[]).find(row=>row.id===id)?.name||id;
 async function mutate(action:string,input:Record<string,unknown>){
  if(busy)return;setBusy(true);setError('');setMessage('');
  const payload={action,...input},signature=JSON.stringify(payload),old=requests.current[action];
  const requestId=old?.signature===signature?old.key:crypto.randomUUID();requests.current[action]={signature,key:requestId};
  try{
   const response=await fetch('/api/stations/erp/purchases',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({...payload,requestId})});
   const result=z.object({success:z.boolean(),error:z.object({message:z.string().optional()}).optional(),data:z.object({id:z.string(),status:z.string()}).optional()}).parse(await response.json());
   if(!response.ok||!result.success||!result.data)throw new Error(result.error?.message||t('Purchase operation failed','採購作業失敗'));
   delete requests.current[action];setMessage(t('Saved','已保存')+' · '+result.data.status);
   if(action==='create'){setSelected(result.data.id);setLines([{itemId:'',quantity:1,unitPrice:0}])}if(action==='receive'){setQuantities({});setBoxes({})}
   try{await onChanged()}catch{setError(t('Saved. Refresh the page to load the latest data.','已保存，請重新整理頁面以取得最新資料。'))}
  }catch(e){setError(e instanceof Error?e.message:t('Purchase operation failed','採購作業失敗'))}finally{setBusy(false)}
 }
 return <section className="panel no-print"><h2>{t('Purchase workflow','採購與收貨')}</h2>
  <p>{t('Create a draft, obtain administrator approval, then receive goods into inventory. Prices use integer minor currency units.','建立草稿、由管理員核准，再將貨品收至庫存。價格使用整數最小貨幣單位。')}</p>
  <form onSubmit={event=>{event.preventDefault();void mutate('create',{supplierId:supplier,lines})}}><fieldset disabled={busy}><legend>{t('New purchase order','新增採購單')}</legend>
   <label>{t('Supplier','供應商')}<select required value={supplier} onChange={e=>setSupplier(e.target.value)}><option value="">{t('Choose supplier','選擇供應商')}</option>{(data.suppliers||[]).filter(row=>row.isActive).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label>
   {lines.map((line,i)=><div className="form-grid" key={i}><label>{t('Purchase item','採購品項')} {i+1}<select required value={line.itemId} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,itemId:e.target.value}:row))}><option value="">{t('Choose item','選擇品項')}</option>{(data.items||[]).filter(row=>row.isActive).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label><label>{t('Order quantity','訂購數量')} {i+1}<input type="number" min={1} max={10000000} step={1} required value={line.quantity} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,quantity:Number(e.target.value)}:row))}/></label><label>{t('Unit price','單價')} {i+1}<input type="number" min={0} max={10000000} step={1} required value={line.unitPrice} onChange={e=>setLines(rows=>rows.map((row,n)=>n===i?{...row,unitPrice:Number(e.target.value)}:row))}/></label>{lines.length>1&&<button type="button" className="secondary" onClick={()=>setLines(rows=>rows.filter((_,n)=>n!==i))}>{t('Remove line','移除明細')} {i+1}</button>}</div>)}
   <button type="button" className="secondary" disabled={lines.length>=100} onClick={()=>setLines(rows=>[...rows,{itemId:'',quantity:1,unitPrice:0}])}>{t('Add purchase item','新增採購品項')}</button> <button type="submit">{t('Create draft','建立草稿')}</button>
  </fieldset></form>
  <fieldset disabled={busy}><legend>{t('Approval and receiving','核准與收貨')}</legend><label>{t('Purchase order','採購單')}<select value={selected} onChange={e=>{setSelected(e.target.value);setQuantities({});setBoxes({});setReason('')}}><option value="">{t('Choose order','選擇採購單')}</option>{(data.purchaseOrders||[]).map(row=><option key={row.id} value={row.id}>{row.poNumber} · {row.status}</option>)}</select></label>
   {order&&<><p>{t('Status','狀態')}: <strong>{order.status}</strong></p>{role==='admin'&&order.status==='draft'&&<div className="form-grid"><button type="button" onClick={()=>void mutate('approve',{poId:selected})}>{t('Approve order','核准採購單')}</button><label>{t('Rejection reason','駁回原因')}<input maxLength={500} value={reason} onChange={e=>setReason(e.target.value)}/></label><button type="button" disabled={!reason.trim()} onClick={()=>void mutate('reject',{poId:selected,reason})}>{t('Reject order','駁回採購單')}</button></div>}
    {order.status==='approved'&&<form onSubmit={e=>{e.preventDefault();void mutate('receive',{poId:selected,warehouseId:warehouse,lines:orderLines.filter(row=>(quantities[row.id]||0)>0).map(row=>({lineId:row.id,quantity:quantities[row.id],isBoxes:Boolean(boxes[row.id])}))})}}><label>{t('Warehouse','倉庫')}<select required value={warehouse} onChange={e=>setWarehouse(e.target.value)}><option value="">{t('Choose warehouse','選擇倉庫')}</option>{(data.warehouses||[]).map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</select></label>
     {orderLines.map((row,i)=><div className="form-grid" key={row.id}><p>{itemName(row.itemId)} · {t('Remaining pieces','未收件數')}: {row.quantity-row.receivedQuantity}</p><label>{t('Receipt quantity','本次收貨數量')} {i+1}<input type="number" min={0} max={10000000} step={1} value={quantities[row.id]||0} onChange={e=>setQuantities(values=>({...values,[row.id]:Number(e.target.value)}))}/></label><label><input type="checkbox" checked={Boolean(boxes[row.id])} onChange={e=>setBoxes(values=>({...values,[row.id]:e.target.checked}))}/>{t('Quantity in boxes','數量單位為箱')}</label></div>)}<button type="submit">{t('Receive goods','確認收貨')}</button>
    </form>}
   </>}
  </fieldset>{error&&<p role="alert" className="error">{error}</p>}{message&&<p role="status">{message}</p>}
 </section>
}