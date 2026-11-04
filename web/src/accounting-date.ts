export function taipeiAccountingDate(now=new Date()):string {
 const parts=new Intl.DateTimeFormat('en-US',{timeZone:'Asia/Taipei',year:'numeric',month:'2-digit',day:'2-digit'}).formatToParts(now);
 const value=(type:string)=>parts.find(p=>p.type===type)!.value;
 return `${value('year')}-${value('month')}-${value('day')}`;
}