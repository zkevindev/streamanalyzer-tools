// Build ./cmd/server and ./internal/diagnostic/testdata/rtsp-stall-fixture.
// Set STREAMANALYZER_TEST_SERVER and STREAMANALYZER_RTSP_STALL_FIXTURE.
// Requires Chrome, FFmpeg and FFprobe; performs actual decoding and offline source replay.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {createHash} = require('node:crypto');
const {setTimeout: delay} = require('node:timers/promises');
async function until(fn, seconds=20) {for(let i=0;i<seconds*10;i++){const value=await fn();if(value)return value;await delay(100);}throw new Error('Timed out');}
async function stop(child) {if(!child||child.exitCode!==null)return;child.kill('SIGTERM');await Promise.race([once(child,'exit'),delay(2000)]);if(child.exitCode===null)child.kill('SIGKILL');}
(async()=>{
 const binary=process.env.STREAMANALYZER_TEST_SERVER,fixtureBinary=process.env.STREAMANALYZER_RTSP_STALL_FIXTURE;
 assert.ok(binary&&fixtureBinary,'STREAMANALYZER_TEST_SERVER and STREAMANALYZER_RTSP_STALL_FIXTURE are required');await fs.access(binary);await fs.access(fixtureBinary);
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-automatic-browser-'));
 const providerCalls=[],providerErrors=[];
 const provider=http.createServer(async(req,res)=>{
  try {
   let body='';for await(const chunk of req)body+=chunk;const request=JSON.parse(body),content=request.messages.find(v=>v.role==='user').content;
   assert.ok(Array.isArray(content),'automatic summary did not use image input');const input=JSON.parse(content[0].text),images=content.filter(v=>v.type==='image_url');assert.ok(images.length>0&&images.length<=3);assert.ok(input.stall_investigations.length>0);
   const measured=[];for(let i=0;i<images.length;i++){const data=Buffer.from(images[i].image_url.url.split(',')[1],'base64'),ref=input.image_evidence[i];assert.equal(data[0],255);assert.equal(data[1],216);assert.equal(data.length,ref.bytes);assert.equal(createHash('sha256').update(data).digest('hex'),ref.sha256);measured.push({id:ref.id,bytes:data.length,sha256:ref.sha256});}
   assert.ok(input.stall_investigations.every(v=>v.buffer_samples.length>0&&v.rtsp_media));assert.ok(!body.includes('rtsp://')&&!body.includes('source_ip'));providerCalls.push({images:measured,cases:input.stall_investigations.length});
   const answer={summary:'已自动提取关键异常并统一核对画面与时序',hypotheses:[{priority:'P1',claim:'先核对异常附近源媒体与接收间隔',evidence_ids:[input.image_evidence[0].id],next_check:'对照同次采样的接收和缓冲记录'}]};
   res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify(answer)}}]}));
  }catch(error){providerErrors.push(error.message);res.writeHead(500,{'Content-Type':'application/json'});res.end(JSON.stringify({error:'fixture assertion failed'}));}
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_IMAGE_INPUT:'auto',STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'automatic-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:['ignore','pipe','pipe']});
 let logs='';for(const pipe of [app.stdout,app.stderr])pipe.on('data',data=>{logs=(logs+data).slice(-6000);});
 let chrome,ws,fixture;
 const api=async(method,url,body)=>{const response=await fetch(base+url,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const value=await response.json();assert.ok(response.ok,JSON.stringify(value));return value;};
 try{
  await until(async()=>{try{return(await fetch(base+'/api/diagnostics/config')).ok;}catch{return false;}});
  const profile=path.join(directory,'chrome');
  chrome=spawn(process.env.CHROME_BIN||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--headless=new','--no-first-run','--autoplay-policy=no-user-gesture-required',`--user-data-dir=${profile}`,'--remote-debugging-port=0','about:blank'],{stdio:'ignore'});
  const debug=await until(()=>fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8').catch(()=>''));
  const tabs=await(await fetch(`http://127.0.0.1:${debug.split('\n')[0]}/json/list`)).json();
  ws=new WebSocket(tabs.find(tab=>tab.type==='page').webSocketDebuggerUrl);await once(ws,'open');
  const pending=new Map();let sequence=0;const errors=[];
  ws.addEventListener('message',({data})=>{const message=JSON.parse(data);if(message.id){const item=pending.get(message.id);pending.delete(message.id);message.error?item.reject(message.error):item.resolve(message.result);}else if(message.method==='Runtime.exceptionThrown')errors.push(message.params);});
  const cdp=(method,params={})=>new Promise((resolve,reject)=>{pending.set(++sequence,{resolve,reject});ws.send(JSON.stringify({id:sequence,method,params}));});
  const evaluate=async expression=>{const result=await cdp('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(result.exceptionDetails)throw new Error(JSON.stringify(result.exceptionDetails));return result.result.value;};
  await cdp('Runtime.enable');
  await cdp('Page.navigate',{url:base+'/diagnostics'});await until(()=>evaluate(`typeof renderRTSPReport==='function'`).catch(()=>false));
  const bytesAt=async url=>Buffer.from(await(await fetch(base+url)).arrayBuffer());
  await evaluate(`window.automaticPosts=[];window.finalPlayerHeld=false;window.finalPlayerReleased=false;window.finalPlayerRelease=()=>{finalPlayerReleased=true;window.releaseFinalPlayer?.();};const rawFetch=window.fetch.bind(window);window.fetch=async(url,options={})=>{if(options.method==='POST'&&typeof options.body==='string'){let body;try{body=JSON.parse(options.body);}catch{}automaticPosts.push({url:String(url),body});if(String(url).endsWith('/player')&&body?.complete&&!finalPlayerReleased){finalPlayerHeld=true;await new Promise(resolve=>{releaseFinalPlayer=resolve;});}}return rawFetch(url,options);};`);
  fixture=spawn(fixtureBinary,[],{stdio:['ignore','pipe','inherit']});let ready='';const source=await new Promise((resolve,reject)=>{fixture.once('error',reject);fixture.stdout.on('data',value=>{ready+=value;const end=ready.indexOf('\n');if(end>=0)resolve(JSON.parse(ready.slice(0,end)));});});
  await evaluate(`document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(source.url)};document.getElementById('seconds').value='12';document.getElementById('analyze-playback').checked=true;document.getElementById('analyze-playback').dispatchEvent(new Event('change'));document.getElementById('sample').requestSubmit()`);
  const run=await until(async()=>{const list=await api('GET','/api/diagnostics/runs');return list.find(v=>v.mode==='sample');});
  assert.equal(await evaluate(`automaticPosts.find(v=>v.url.endsWith('/runs')).body.auto_investigate`),true,'default UI opt-in was not submitted');
  const waiting=await until(async()=>{const detail=await api('GET','/api/diagnostics/runs/'+run.id);return detail.status!=='running'&&detail.report?.automatic_investigation?.state==='waiting_player'?detail:false;},24);
  const stats=await(await fetch(source.status_url)).json();assert.equal(stats.plays,1);await stop(fixture);fixture=undefined;
  assert.equal(waiting.report.automatic_investigation.selected.length,0);assert.equal(providerCalls.length,0);await until(()=>evaluate('finalPlayerHeld'),8);
  const beforeFinal=await api('GET','/api/diagnostics/runs/'+run.id);assert.equal(beforeFinal.report.automatic_investigation.state,'waiting_player');assert.equal(beforeFinal.automatic_running,true);await evaluate('finalPlayerRelease()');
  const complete=await until(async()=>{const detail=await api('GET','/api/diagnostics/runs/'+run.id);return ['complete','partial'].includes(detail.report?.automatic_investigation?.state)&&detail.report.automatic_investigation.model_state==='complete'&&detail.automatic_running!==true?detail:false;},80);
  const automatic=complete.report.automatic_investigation,cases=complete.report.stall_investigations,summary=complete.explanation,audit=summary?.report_input_evidence;
  assert.ok(automatic.candidate_count>=automatic.selected.length&&automatic.selected.length>0&&automatic.selected.length<=3);assert.equal(cases.length,automatic.selected.length);assert.equal(audit.mode,'vision');assert.equal(automatic.summary_report_sha256,audit.report_sha256);assert.equal(summary.hypotheses[0].priority,'P1');assert.equal(providerCalls.length,1,'automatic workflow called a model more than once');
  const assets={};for(const item of cases){assert.equal(item.model_state,'not_requested');assert.ok(!item.explanation);assert.equal(item.media.assets.length,4,JSON.stringify(item.media));assert.ok(automatic.selected.some(v=>v.case_id===item.id));for(const asset of item.media.assets){const data=await bytesAt(`/api/diagnostics/runs/${run.id}/stall-assets/${item.id}/${asset.name.split('/').at(-1)}`);assert.equal(createHash('sha256').update(data).digest('hex'),asset.sha256);assets[asset.name]=data;if(asset.kind==='screenshot'){const dimensions=await evaluate(`new Promise((resolve,reject)=>{const image=new Image();image.onload=()=>resolve([image.naturalWidth,image.naturalHeight]);image.onerror=reject;image.src='data:image/jpeg;base64,'+${JSON.stringify(data.toString('base64'))};})`);assert.ok(dimensions[0]>=160&&dimensions[1]>=90);}else{const clipURL=base+`/api/diagnostics/runs/${run.id}/stall-assets/${item.id}/clip.mp4`;await evaluate(`window.autoClipProbe=document.createElement('video');autoClipProbe.muted=true;autoClipProbe.autoplay=true;autoClipProbe.src=${JSON.stringify(clipURL)};document.body.append(autoClipProbe);autoClipProbe.play().catch(()=>{});`);const decoded=await until(()=>evaluate(`autoClipProbe.currentTime>0.2&&autoClipProbe.videoWidth>0&&autoClipProbe.webkitAudioDecodedByteCount>0?{frames:autoClipProbe.getVideoPlaybackQuality().totalVideoFrames,audio:autoClipProbe.webkitAudioDecodedByteCount}:false`),10);assert.ok(decoded.frames>0&&decoded.audio>0);await evaluate('autoClipProbe.pause();autoClipProbe.remove();');}}}
  assert.equal(await evaluate(`automaticPosts.filter(v=>v.url.includes('/stalls')).length`),0,'test or UI manually posted a case');assert.equal(await evaluate(`automaticPosts.filter(v=>v.url.includes('/explain')).length`),0,'test or UI manually requested the global model');
  await until(()=>evaluate(`document.getElementById('automatic-investigation-content').textContent.includes('整体汇总状态已生成')&&document.querySelector('.report-input-evidence')?.textContent.includes('本次为整份报告图文解读')`));
  assert.match(await evaluate(`document.getElementById('stall-investigation-results').textContent`),/未单独解读/);assert.equal(await evaluate(`new Set(Array.from(document.querySelectorAll('[id]')).map(v=>v.id)).size===document.querySelectorAll('[id]').length`),true);
  const raw=await bytesAt(`/api/diagnostics/runs/${run.id}/dump`),bundle=await bytesAt(`/api/diagnostics/runs/${run.id}/evidence.zip`);const html=await(await fetch(base+`/api/diagnostics/runs/${run.id}/report.html`)).text();assert.ok(html.includes('自动补查与整体汇总')&&html.includes('本次为整份报告图文解读')&&html.includes('未单独解读'));
  const archive=path.join(directory,'automatic.zip');await fs.writeFile(archive,bundle);const doc=await cdp('DOM.getDocument'),input=await cdp('DOM.querySelector',{nodeId:doc.root.nodeId,selector:'#archive'});await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[archive]});await evaluate(`document.getElementById('import').requestSubmit()`);
  const imported=await until(async()=>{const list=await api('GET','/api/diagnostics/runs');return list.find(v=>v.mode==='replay');}),replay=await api('GET','/api/diagnostics/runs/'+imported.id);assert.deepEqual(replay.report,complete.report);assert.deepEqual(replay.explanation,summary);assert.notEqual(replay.automatic_running,true);assert.ok(raw.equals(await bytesAt(`/api/diagnostics/runs/${imported.id}/dump`)));
  for(const item of cases)for(const asset of item.media.assets)assert.ok(assets[asset.name].equals(await bytesAt(`/api/diagnostics/runs/${imported.id}/stall-assets/${item.id}/${asset.name.split('/').at(-1)}`)));
  await until(()=>evaluate(`document.getElementById('automatic-investigation-content').textContent.includes('已导入记录，不自动续跑')`));await delay(1200);assert.equal(providerCalls.length,1,'offline import re-executed model work');assert.equal(providerErrors.length,0,JSON.stringify(providerErrors));assert.equal(errors.length,0,JSON.stringify(errors));
  if(process.env.STREAMANALYZER_AUTOMATIC_ARCHIVE)await fs.writeFile(process.env.STREAMANALYZER_AUTOMATIC_ARCHIVE,bundle);
  console.log(JSON.stringify({result:'PASS',id:run.id,replay:imported.id,plays:stats.plays,manual_case_posts:0,manual_model_posts:0,source_stopped_before_final_player:true,automatic,model_requests:providerCalls,exact_report_explanation_raw_assets:true}));
 }catch(error){console.error(logs);throw error;}
 finally{ws?.close();await stop(fixture);await stop(chrome);await stop(app);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
