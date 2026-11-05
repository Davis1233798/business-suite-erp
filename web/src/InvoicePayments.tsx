import React,{useRef,useState} from 'react';
import {tr} from './locale';
type Row=Record<string,any>;
const maxMoney=9007199254740991;
export function InvoicePayments({data,onChanged}:{data:Record<string,Row[]>;onChanged:()=>Promise<void>}){
 const [kind,setKind]=useState('payable'),[order,setOrder]=useState(''),[due,setDue]=useState(''),[invoice,setInvoice]=useState(''),[amount,setAmount]=useState('1'),[method,setMethod]=useState('bank_transfer'),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[error,setError]=useState('');
 const retry=useRef({signature:'',key:''});
 const orders=(kind==='payable'?data.purchaseOrders:data.salesOrders)||[];
 const eligible=orders.filter(row=>row.status===(kind==='payable'?'received':'shipped'));
 const invoices=(data.invoices||[]).filter(row=>row.status==='unpaid'||row.status==='partially_paid');
 const selected=invoices.find(row=>row.id===invoice);
 const balance=selected?Number(selected.totalAmount)-Number(selected.paidAmount):0;
 const paymentAmount=Number(amount);
 const validPayment=!!selected&&Number.isSafeInteger(paymentAmount)&&paymentAmount>0&&paymentAmount<=balance&&paymentAmount<=maxMoney;
 async function send(payload:Record<string,unknown>){
  setBusy(true);setError('');setMessage('');
  const signature=JSON.stringify(payload);
  if(retry.current.signature!==signature)retry.current={signature,key:crypto.randomUUID()};
  try{
   const response=await fetch('/api/stations/erp/finance',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({...payload,idempotencyKey:retry.current.key})});
   const body=await response.json();
   if(!response.ok||!body.success)throw new Error(body.error?.message||`HTTP ${response.status}`);
   retry.current={signature:'',key:''};
   setMessage(tr(payload.action==='invoice'?'Internal invoice created':'Payment recorded'));
   if(payload.action==='invoice')setOrder('');else{setInvoice('');setAmount('1')}
   try{await onChanged()}catch{setError(tr('Saved, but the display could not be refreshed. Reload before submitting another operation.'))}
  }catch(reason){setError((reason as Error).message)}finally{setBusy(false)}
 }
 return <section className="panel" role="region" aria-label={tr('Invoices and payments')}>
  <h2>{tr('Invoices and payments')}</h2><p>{tr('Internal ledger records only; no tax invoice or bank transfer is issued.')}</p>
  <form className="form-grid" onSubmit={event=>{event.preventDefault();if(!busy&&eligible.some(row=>row.id===order))void send({action:'invoice',invoiceKind:kind,orderId:order,dueDate:due})}}>
   <label>{tr('Invoice type')}<select value={kind} disabled={busy} onChange={event=>{setKind(event.target.value);setOrder('')}}><option value="payable">{tr('Accounts payable')}</option><option value="receivable">{tr('Accounts receivable')}</option></select></label>
   <label>{tr('Completed source order')}<select required value={order} disabled={busy} onChange={event=>setOrder(event.target.value)}><option value="">{tr('Select an item')}</option>{eligible.map(row=><option key={row.id} value={row.id}>{row.poNumber||row.soNumber||row.id}</option>)}</select></label>
   <label>{tr('Due date')}<input type="date" value={due} disabled={busy} onChange={event=>setDue(event.target.value)}/></label>
   <button disabled={busy||!eligible.some(row=>row.id===order)}>{tr('Create internal invoice')}</button>
  </form>
  <form className="form-grid" onSubmit={event=>{event.preventDefault();if(!busy&&validPayment)void send({action:'payment',invoiceId:invoice,amount:paymentAmount,method})}}>
   <label>{tr('Open invoice')}<select required value={invoice} disabled={busy} onChange={event=>setInvoice(event.target.value)}><option value="">{tr('Select an item')}</option>{invoices.map(row=><option key={row.id} value={row.id}>{row.invoiceNumber} · {Number(row.totalAmount)-Number(row.paidAmount)}</option>)}</select></label>
   <label>{tr('Payment amount')}<input type="number" required min={1} max={Math.min(maxMoney,balance||maxMoney)} step={1} value={amount} disabled={busy} onChange={event=>setAmount(event.target.value)}/></label>
   <label>{tr('Payment method')}<select value={method} disabled={busy} onChange={event=>setMethod(event.target.value)}><option value="bank_transfer">{tr('Bank transfer')}</option><option value="cash">{tr('Cash')}</option></select></label>
   <button disabled={busy||!validPayment}>{tr('Record payment')}</button>
  </form>
  {message&&<p role="status">{message}</p>}{error&&<p role="alert" className="error">{error}</p>}
 </section>;
}
