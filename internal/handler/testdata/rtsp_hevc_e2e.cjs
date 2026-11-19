// Build ./cmd/server and ./internal/diagnostic/testdata/h265-fixture first.
// Set STREAMANALYZER_TEST_SERVER and STREAMANALYZER_HEVC_FIXTURE to binaries.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {setTimeout: delay} = require('node:timers/promises');
async function until(fn, seconds=20) {for(let i=0;i<seconds*10;i++){const value=await fn();if(value)return value;await delay(100);}throw new Error('Timed out');}
async function stop(child) {if(!child||child.exitCode!==null)return;child.kill('SIGTERM');await Promise.race([once(child,'exit'),delay(2000)]);if(child.exitCode===null)child.kill('SIGKILL');}
(async()=>{
 const binary=process.env.STREAMANALYZER_TEST_SERVER,fixtureBinary=process.env.STREAMANALYZER_HEVC_FIXTURE;
 assert.ok(binary&&fixtureBinary,'STREAMANALYZER_TEST_SERVER and STREAMANALYZER_HEVC_FIXTURE are required');
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-hevc-browser-'));
 const providerCalls=[];
 const provider=http.createServer(async(req,res)=>{
  let body='';for await(const chunk of req)body+=chunk;
  providerCalls.push(body);
  const messages=JSON.parse(body).messages.map(message=>message.content||'').join('\n');
  assert.ok(messages.includes('"video_codec":"h265"')&&messages.includes('"video_output_codec":"h265"')&&messages.includes('native_remux'));
  for(const secret of ['rtsp://','source_ip','127.0.0.1'])assert.ok(!body.includes(secret),secret);
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify({summary:'已核对 HEVC 原始接收与本机预览证据',hypotheses:[]})}}]}));
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'hevc-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:['ignore','pipe','pipe']});
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
  await cdp('Runtime.enable');await cdp('Page.navigate',{url:base+'/diagnostics'});await until(()=>evaluate(`typeof renderRTSPReport==='function'`).catch(()=>false));
  const results=[];
  for(const transport of ['tcp','udp'])for(const mode of ['hevc']){
   fixture=spawn(fixtureBinary,['-input',path.resolve(__dirname,'../../diagnostic/testdata/preview-h265.h265')],{stdio:['ignore','pipe','inherit']});
   let ready='';const source=await new Promise((resolve,reject)=>{fixture.once('error',reject);fixture.once('exit',code=>reject(new Error('fixture exited '+code)));fixture.stdout.on('data',chunk=>{ready+=chunk;const end=ready.indexOf('\n');if(end>=0){try{resolve(JSON.parse(ready.slice(0,end)));}catch(error){reject(error);}}});});
   await evaluate(`window.hevcObserved={frames:0,presented:0,time:0};clearInterval(window.hevcObservationTimer);window.hevcObservationTimer=setInterval(()=>{const video=document.getElementById('sample-player');hevcObserved.frames=Math.max(hevcObserved.frames,Number(video.getVideoPlaybackQuality?.().totalVideoFrames)||0);hevcObserved.time=Math.max(hevcObserved.time,video.currentTime||0);hevcObserved.presented=Math.max(hevcObserved.presented,Number(activePlayer?.collector?.samples?.at(-1)?.video?.presented_frames)||0);},50);document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('rtsp-transport').value=${JSON.stringify(transport)};document.getElementById('rtsp-transport').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(source.url)};document.getElementById('seconds').value='6';document.getElementById('analyze-playback').checked=true;document.getElementById('auto-investigate').checked=false;document.getElementById('sample').requestSubmit()`);
   const run=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='sample'&&!results.some(done=>done.id===item.id));});
   const decoded=await until(()=>evaluate(`hevcObserved.frames>0&&hevcObserved.presented>0&&hevcObserved.time>0.25?hevcObserved:false`),12);
   const detail=await until(async()=>{const value=await api('GET','/api/diagnostics/runs/'+run.id);return value.report?.player?.state==='complete'?value:false;},15);
   const finalDecoded=await evaluate('hevcObserved');
   assert.ok(finalDecoded.frames>=30&&finalDecoded.frames>decoded.frames+10&&finalDecoded.presented>0&&finalDecoded.time>2,'HEVC frames did not continue decoding');
   const preview=detail.report.rtsp.preview;
   assert.equal(preview.version,2);assert.equal(preview.codec_capabilities_version,1);assert.equal(preview.video_codec,'h265');assert.equal(preview.video_output_codec,'h265');assert.equal(preview.video_transform,'native_remux');assert.equal(preview.state,'ready');assert.ok(preview.video_aus>30);assert.equal(preview.packet_errors,0);
   assert.ok(detail.report.player.first_screen_ms!=null&&detail.report.player.video.presented_frames>0);assert.equal(detail.report.rtsp.transport,transport==='tcp'?'tcp_interleaved':'udp');
   const counted=await(await fetch(source.status_url)).json();assert.equal(counted.plays,1,'preview opened a second RTSP pull');assert.equal(counted[transport],1);
   await evaluate(`document.getElementById('explain').click()`);await until(()=>evaluate(`document.getElementById('explanation').textContent.includes('已核对 HEVC')`));
   const raw=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/dump`)).arrayBuffer());
   const html=await(await fetch(base+`/api/diagnostics/runs/${run.id}/report.html`)).text();assert.ok(html.includes('h265'));assert.ok(html.includes('原生转封装'));
   const bundle=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/evidence.zip`)).arrayBuffer());
   await stop(fixture);fixture=undefined;
   const handle=await cdp('DOM.getDocument'),input=await cdp('DOM.querySelector',{nodeId:handle.root.nodeId,selector:'#archive'});
   const bundlePath=path.join(directory,`${transport}-${mode}.zip`);await fs.writeFile(bundlePath,bundle);await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[bundlePath]});await evaluate(`document.getElementById('import').requestSubmit()`);
   const imported=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='replay'&&!results.some(done=>done.replay===item.id));});
   const replay=await api('GET','/api/diagnostics/runs/'+imported.id);assert.deepEqual(replay.report,detail.report);assert.ok(raw.equals(Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${imported.id}/dump`)).arrayBuffer())));
   results.push({id:run.id,replay:imported.id,transport,mode,decoded_frames:finalDecoded.frames,presented_frames:finalDecoded.presented,media_time:finalDecoded.time,video_aus:preview.video_aus,plays:counted.plays});
  }
  assert.equal(errors.length,0,JSON.stringify(errors));assert.equal(providerCalls.length,2);console.log(JSON.stringify({result:'PASS',results,model_calls:providerCalls.length}));
 }catch(error){console.error(logs);throw error;}
 finally{ws?.close();await stop(fixture);await stop(chrome);await stop(app);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
