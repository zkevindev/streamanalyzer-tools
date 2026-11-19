// Run after go build -o /path/server ./cmd/server; set STREAMANALYZER_TEST_SERVER.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {setTimeout: delay} = require('node:timers/promises');
async function until(fn, seconds=20) { for(let i=0;i<seconds*10;i++) { const v=await fn(); if(v)return v; await delay(100); } throw new Error('Timed out'); }
(async()=>{
 const binary=process.env.STREAMANALYZER_TEST_SERVER;
 assert.ok(binary,'STREAMANALYZER_TEST_SERVER is required');
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-rtsp-udp-e2e-'));
 const providerCalls=[];
 const provider=http.createServer(async(req,res)=>{
  let body='';for await(const part of req)body+=part;
  providerCalls.push(body);
  assert.ok(body.includes('native_rtsp_udp_receive'));
  for(const secret of ['udp-camera-password','udp-camera-user','rtsp://','source_ip','127.0.0.1'])assert.ok(!body.includes(secret),secret);
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify({summary:'已核对 UDP 接收与控制阶段证据',hypotheses:[]})}}]}));
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'udp-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:'ignore'});
 let chrome,ws;
 const api=async(method,url,body)=>{const response=await fetch(base+url,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const data=await response.json();assert.ok(response.ok,JSON.stringify(data));return data;};
 try{
  await until(async()=>{try{return(await fetch(base+'/api/diagnostics/config')).ok;}catch{return false;}});
  const profile=path.join(directory,'chrome');
  chrome=spawn(process.env.CHROME_BIN||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--headless=new','--disable-gpu','--no-first-run',`--user-data-dir=${profile}`,'--remote-debugging-port=0','about:blank'],{stdio:'ignore'});
  const debug=await until(()=>fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8').catch(()=>''));
  const tabs=await(await fetch(`http://127.0.0.1:${debug.split('\n')[0]}/json/list`)).json();
  ws=new WebSocket(tabs.find(t=>t.type==='page').webSocketDebuggerUrl);await once(ws,'open');
  const pending=new Map();let number=0;const errors=[];
  ws.addEventListener('message',({data})=>{const message=JSON.parse(data);if(message.id){const item=pending.get(message.id);pending.delete(message.id);message.error?item.reject(message.error):item.resolve(message.result);}else if(message.method==='Runtime.exceptionThrown')errors.push(message.params);});
  const cdp=(method,params={})=>new Promise((resolve,reject)=>{pending.set(++number,{resolve,reject});ws.send(JSON.stringify({id:number,method,params}));});
  const evaluate=async expression=>{const result=await cdp('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(result.exceptionDetails)throw new Error(JSON.stringify(result.exceptionDetails));return result.result.value;};
  await cdp('Runtime.enable');await cdp('Page.navigate',{url:base+'/diagnostics'});await until(()=>evaluate(`typeof renderRTSPReport==='function'`).catch(()=>false));
  const results=[];
  for(const silent of [false,true]){
   const status=await api('POST','/api/simulators/onvif/start',{bind_ip:'127.0.0.1',silent_udp:silent,rtsp_username:'udp-camera-user',rtsp_password:'udp-camera-password'});
   const target=new URL(status.stream_url);target.username='udp-camera-user';target.password='udp-camera-password';
   await evaluate(`document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('rtsp-transport').value='udp';document.getElementById('rtsp-transport').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(target.href)};document.getElementById('seconds').value='4';document.getElementById('sample').requestSubmit()`);
   const run=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');const item=runs.find(r=>!results.some(x=>x.id===r.id));return item?.report_ready?item:false;});
   const detail=await api('GET','/api/diagnostics/runs/'+run.id);assert.equal(detail.report.rtsp.transport,'udp');
   if(silent){assert.equal(detail.report.rtsp.rtp_packets,0);assert.ok(detail.report.findings.some(f=>f.code==='rtsp_udp_no_rtp'));}else assert.ok(detail.report.rtsp.rtp_packets>0);
   await until(()=>evaluate(`document.getElementById('dump').href.endsWith('/runs/${run.id}/dump')`));
   await until(()=>evaluate(`document.getElementById('rtsp-metrics').textContent.includes('UDP 单播 RTP/RTCP')`));
   const raw=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/dump`)).arrayBuffer());
   const html=await(await fetch(base+`/api/diagnostics/runs/${run.id}/report.html`)).text();assert.ok(html.includes('udp-datagrams.bin'));assert.ok(!html.includes('udp-camera-password'));
   await evaluate(`document.getElementById('explain').click()`);await until(()=>evaluate(`document.getElementById('explanation').textContent.includes('已核对 UDP')`));
   const bundle=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/evidence.zip`)).arrayBuffer());
   await api('POST','/api/simulators/onvif/stop',{});
   const handle=await cdp('DOM.getDocument');const input=await cdp('DOM.querySelector',{nodeId:handle.root.nodeId,selector:'#archive'});const bundlePath=path.join(directory,silent?'silent.zip':'healthy.zip');await fs.writeFile(bundlePath,bundle);await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[bundlePath]});await evaluate(`document.getElementById('import').requestSubmit()`);
   const imported=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(r=>r.mode==='replay'&&!results.some(x=>x.replay===r.id));});
   const replay=await api('GET','/api/diagnostics/runs/'+imported.id);assert.deepEqual(replay.report,detail.report);const copy=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${imported.id}/dump`)).arrayBuffer());assert.ok(raw.equals(copy));
   results.push({id:run.id,replay:imported.id,silent,rtp_packets:detail.report.rtsp.rtp_packets,udp_datagrams:detail.report.rtsp.udp_datagrams||0});
  }
  assert.equal(errors.length,0,JSON.stringify(errors));assert.equal(providerCalls.length,2);console.log(JSON.stringify({result:'PASS',results,model_calls:providerCalls.length}));
 } finally {ws?.close();chrome?.kill('SIGTERM');app.kill('SIGTERM');await delay(500);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
