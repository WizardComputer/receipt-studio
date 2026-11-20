// Prepare terminal demo history. Installation requires a stopped, locked agent.
// No print requests or network connections are made. Reprints target loopback port 1.
import {open} from 'node:fs/promises';
import {DatabaseSync} from 'node:sqlite';
import {compile,initialTemplate,sampleData} from '../src/core.js';
const [databasePath,output,countArg='24']=process.argv.slice(2);
if(!databasePath||!output)throw new Error('Usage: node scripts/prepare-demo-jobs.mjs AGENT_DATABASE OUTPUT_SQLITE [COUNT]');
const count=Number(countArg);
if(!Number.isInteger(count)||count<1||count>5000)throw new Error('COUNT must be an integer between 1 and 5000.');
const source=new DatabaseSync(databasePath,{readOnly:true});
let entry, schema;
try {
schema=source.prepare("SELECT sql FROM sqlite_master WHERE name IN ('jobs','job_receipts','jobs_created_id','jobs_status_created_id') ORDER BY type DESC,name").all().map(row=>row.sql).join(';');
if(!source.prepare("PRAGMA table_info(jobs)").all().some(column=>column.name==='status'))throw new Error('The source database must use the current job schema.');
const defaultPrinter=source.prepare("SELECT value FROM settings WHERE key='default-printer'").get()?.value;
const preferred=source.prepare('SELECT template_id FROM printers WHERE id=?').get(String(defaultPrinter||''))?.template_id;
const row=source.prepare('SELECT v.template,v.sample_data,v.version FROM templates t JOIN template_versions v ON v.template_id=t.id AND v.version=t.current_version WHERE t.archived=0 ORDER BY (t.id=?) DESC,t.position,t.id LIMIT 1').get(preferred||'');
entry=row?{template:JSON.parse(Buffer.from(row.template).toString()),sample_data:JSON.parse(Buffer.from(row.sample_data).toString()),version:row.version}:{template:initialTemplate(),sample_data:sampleData,version:1};
} finally {source.close();}
const products=[['Domaća kafa',250],['Čaj od šipka i šumsko voće',350],['Sendvič s povrćem',650],['Cijeđeni sok od narandže',500],['Čokoladni kolač',450],['Mineralna voda',200],['Doručak s domaćim sirom i svježim pecivom',1200]];
const money=c=>(c/100).toFixed(2).replace('.',',');
const now=new Date();const batch=now.getTime();
// Refuse to overwrite either an agent database or an earlier demo export.
const file=await open(output,'wx',0o600);await file.close();
const db=new DatabaseSync(output);
db.exec('PRAGMA foreign_keys=ON;'+schema+'; BEGIN');
const insert=db.prepare('INSERT INTO jobs(id,printer_id,printer_name,template_version,status,message,created_at,updated_at,started_at,completed_at,original_job_id,byte_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)');
const insertReceipt=db.prepare('INSERT INTO job_receipts(job_id,connection,queue,host,port,template,data,commands) VALUES(?,?,?,?,?,?,?,?)');
const timestamp=date=>date.toISOString().replace(/(\.\d{3})Z$/,'$1000000Z');
try {
for(let i=0;i<count;i++) {
 const created=new Date(now.getTime()-(Math.floor(i/4)*24+i%4*2)*3600000-60000);
 const started=new Date(+created+100), completed=new Date(+created+500+i*30);
 const items=Array.from({length:1+i%7},(_,j)=>{const [name,cents]=products[(i+j)%products.length],qty=1+(i+j)%3;return {name,qty:String(qty),price:money(cents*qty),unit_price:money(cents),amount:cents*qty}});
 const data={...entry.sample_data,shop:['DEMO - Mala radnja','DEMO - Kafić Centar','DEMO - Pekara'][i%3],number:`DEMO-${String(i+1).padStart(4,'0')}`,date:created.toLocaleString('bs-BA'),items:items.map(({amount,...item})=>item),total:money(items.reduce((n,p)=>n+p.amount,0))+' KM',receipt_url:`https://example.com/receipts/demo-${batch}-${i+1}`};
 const bytes=compile(entry.template,data,{preview:false}).bytes;
 const status=['sent','sent','failed','unknown'][i%4];
 const messages={sent:'Simulirano uspješno slanje.',failed:'Simulirana nedostupnost printera.',unknown:'Simuliran prekid veze tokom slanja.'};
 const id=`demo-${batch}-${String(i+1).padStart(2,'0')}`;
 insert.run(id,'demo-simulator','Demo kasa · bez fizičkog printanja',entry.version,status,'DEMO — '+messages[status],timestamp(created),timestamp(completed),timestamp(started),timestamp(completed),'',bytes.length);
 insertReceipt.run(id,'tcp','','127.0.0.1',1,Buffer.from(JSON.stringify(entry.template)),Buffer.from(JSON.stringify(data)),Buffer.from(bytes));
}
db.exec('COMMIT');
} catch(error) {db.exec('ROLLBACK');throw error;} finally {db.close();}
console.log(`Prepared ${count} demo jobs in a separate SQLite database; no printing performed.`);
