// Build ./cmd/server and ./internal/diagnostic/testdata/opus-fixture first.
// Set STREAMANALYZER_TEST_SERVER and STREAMANALYZER_OPUS_FIXTURE to binaries.
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
 const binary=process.env.STREAMANALYZER_TEST_SERVER,fixtureBinary=process.env.STREAMANALYZER_OPUS_FIXTURE;
 const forceWebM=process.env.STREAMANALYZER_FORCE_OPUS_WEBM==='1';
 assert.ok(binary&&fixtureBinary,'STREAMANALYZER_TEST_SERVER and STREAMANALYZER_OPUS_FIXTURE are required');
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-opus-browser-'));
 const providerCalls=[];
 const provider=http.createServer(async(req,res)=>{
  let body='';for await(const chunk of req)body+=chunk;
  providerCalls.push(body);
  const messages=JSON.parse(body).messages.map(message=>message.content||'').join('\n');
  assert.ok(messages.includes('"audio_codec":"opus"')&&messages.includes('"audio_output_codec":"opus"')&&messages.includes('native_remux'));
  for(const secret of ['rtsp://','source_ip','127.0.0.1'])assert.ok(!body.includes(secret),secret);
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify({summary:'已核对 Opus 原始接收与本机预览证据',hypotheses:[]})}}]}));
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'opus-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:['ignore','pipe','pipe']});
 let logs='';for(const pipe of [app.stdout,app.stderr])pipe.on('data',data=>{logs=(logs+data).slice(-6000);});
 let chrome,ws,fixture;
 const api=async(method,url,body)=>{const response=await fetch(base+url,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const value=await response.json();assert.ok(response.ok,JSON.stringify(value));return value;};
 try{
  await until(async()=>{try{return(await fetch(base+'/api/diagnostics/config')).ok;}catch{return false;}});
  const profile=path.join(directory,'chrome');
  chrome=spawn(process.env.CHROME_BIN||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--headless=new','--disable-gpu','--no-first-run','--autoplay-policy=no-user-gesture-required',`--user-data-dir=${profile}`,'--remote-debugging-port=0','about:blank'],{stdio:'ignore'});
  const debug=await until(()=>fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8').catch(()=>''));
  const tabs=await(await fetch(`http://127.0.0.1:${debug.split('\n')[0]}/json/list`)).json();
  ws=new WebSocket(tabs.find(tab=>tab.type==='page').webSocketDebuggerUrl);await once(ws,'open');
  const pending=new Map();let sequence=0;const errors=[];
  ws.addEventListener('message',({data})=>{const message=JSON.parse(data);if(message.id){const item=pending.get(message.id);pending.delete(message.id);message.error?item.reject(message.error):item.resolve(message.result);}else if(message.method==='Runtime.exceptionThrown')errors.push(message.params);});
  const cdp=(method,params={})=>new Promise((resolve,reject)=>{pending.set(++sequence,{resolve,reject});ws.send(JSON.stringify({id:sequence,method,params}));});
  const evaluate=async expression=>{const result=await cdp('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(result.exceptionDetails)throw new Error(JSON.stringify(result.exceptionDetails));return result.result.value;};
  await cdp('Runtime.enable');
  await cdp('Page.navigate',{url:base+'/diagnostics'});await until(()=>evaluate(`typeof renderRTSPReport==='function'`).catch(()=>false));
  await evaluate(`window.opusMSEContainers=[];for(const source of [window.MediaSource,window.ManagedMediaSource].filter(Boolean)){const supported=source.isTypeSupported.bind(source);if(${JSON.stringify(forceWebM)})source.isTypeSupported=function(type){return type.toLowerCase().includes('audio/mp4')&&type.toLowerCase().includes('opus')?false:supported(type);};const append=source.prototype.addSourceBuffer;source.prototype.addSourceBuffer=function(type){opusMSEContainers.push(type);return append.call(this,type);};}`);
  const results=[];
  for(const transport of ['tcp','udp'])for(const mode of ['common','variants']){
   fixture=spawn(fixtureBinary,['-mode',mode],{stdio:['ignore','pipe','inherit']});
   let ready='';const source=await new Promise((resolve,reject)=>{fixture.once('error',reject);fixture.once('exit',code=>reject(new Error('fixture exited '+code)));fixture.stdout.on('data',chunk=>{ready+=chunk;const end=ready.indexOf('\n');if(end>=0){try{resolve(JSON.parse(ready.slice(0,end)));}catch(error){reject(error);}}});});
   await evaluate(`window.opusMSEContainers=[];window.opusObserved={audio_bytes:0,time:0};clearInterval(window.opusObservationTimer);window.opusObservationTimer=setInterval(()=>{const video=document.getElementById('sample-player');opusObserved.audio_bytes=Math.max(opusObserved.audio_bytes,Number(video.webkitAudioDecodedByteCount)||0);opusObserved.time=Math.max(opusObserved.time,video.currentTime||0);},50);document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('rtsp-transport').value=${JSON.stringify(transport)};document.getElementById('rtsp-transport').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(source.url)};document.getElementById('seconds').value='6';document.getElementById('analyze-playback').checked=true;document.getElementById('auto-investigate').checked=false;document.getElementById('sample-player').muted=false;document.getElementById('sample').requestSubmit()`);
   const run=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='sample'&&!results.some(done=>done.id===item.id));});
   const decoded=await until(()=>evaluate(`opusObserved.audio_bytes>0&&opusObserved.time>0.25?opusObserved:false`),12);
   let dtxGap;
   if(mode==='variants'){
    // MSE exposes the intentionally empty RTP audio interval as a real hole.
    // Do not alter production playback or manufacture encoded silence. This
    // explicit test-user seek is recorded by the collector and verifies that
    // the same native pull resumes decoding after the observed waiting event.
    dtxGap=await until(()=>evaluate(`(()=>{const video=document.getElementById('sample-player'),buffer=video.buffered;if(buffer.length<2||video.currentTime>=buffer.start(1))return false;return {start:buffer.end(0),end:buffer.start(1),at:video.currentTime};})()`),8);
    assert.ok(dtxGap.end-dtxGap.start>=0.18);
    await delay(600);
    await evaluate(`document.getElementById('sample-player').currentTime=${dtxGap.end+0.01};document.getElementById('sample-player').play()`);
   }
   const detail=await until(async()=>{const value=await api('GET','/api/diagnostics/runs/'+run.id);return value.report?.player?.state==='complete'?value:false;},15);
   const finalDecoded=await evaluate('opusObserved');
   assert.ok(finalDecoded.audio_bytes>=20000&&finalDecoded.audio_bytes>decoded.audio_bytes+5000&&finalDecoded.time>2,'audio decoding did not continue through the full fixture: '+JSON.stringify({transport,mode,first:decoded,final:finalDecoded,preview:detail.report.rtsp.preview,player:detail.report.player.state}));
   const preview=detail.report.rtsp.preview;
   const containers=await evaluate('opusMSEContainers');
   assert.ok(containers.some(type=>type.includes(forceWebM?'audio/webm':'audio/mp4')&&type.toLowerCase().includes('opus')),'unexpected actual Opus SourceBuffer container: '+containers);
   assert.equal(preview.version,2);assert.ok(preview.codec_capabilities_version>=2);assert.equal(preview.audio_codec,'opus');assert.equal(preview.audio_output_codec,'opus');assert.equal(preview.audio_transform,'native_remux');assert.equal(preview.state,'ready');assert.ok(preview.audio_aus>20&&preview.video_aus>10);assert.equal(preview.packet_errors,0);
   assert.ok(detail.report.player.first_screen_ms!=null);assert.equal(detail.report.rtsp.transport,transport==='tcp'?'tcp_interleaved':'udp');
   if(mode==='variants'){const audio=detail.report.rtsp.tracks.find(track=>track.kind==='audio');assert.ok(audio.max_arrival_gap_ms>=180,'DTX gap not observed in original RTP');}
   const counted=await(await fetch(source.status_url)).json();assert.equal(counted.plays,1,'preview opened a second RTSP pull');assert.equal(counted[transport],1);
   await evaluate(`document.getElementById('explain').click()`);await until(()=>evaluate(`document.getElementById('explanation').textContent.includes('已核对 Opus')`));
   const raw=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/dump`)).arrayBuffer());
   const html=await(await fetch(base+`/api/diagnostics/runs/${run.id}/report.html`)).text();assert.ok(html.includes('opus')||html.includes('Opus'));
   const bundle=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/evidence.zip`)).arrayBuffer());
   await stop(fixture);fixture=undefined;
   const handle=await cdp('DOM.getDocument'),input=await cdp('DOM.querySelector',{nodeId:handle.root.nodeId,selector:'#archive'});
   const bundlePath=path.join(directory,`${transport}-${mode}.zip`);await fs.writeFile(bundlePath,bundle);await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[bundlePath]});await evaluate(`document.getElementById('import').requestSubmit()`);
   const imported=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='replay'&&!results.some(done=>done.replay===item.id));});
   const replay=await api('GET','/api/diagnostics/runs/'+imported.id);assert.deepEqual(replay.report,detail.report);assert.ok(raw.equals(Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${imported.id}/dump`)).arrayBuffer())));
   if(mode==='variants')assert.ok(detail.report.player.events.some(event=>event.kind==='seeking'),'explicit DTX test seek was not recorded');
   results.push({id:run.id,replay:imported.id,transport,mode,audio_decoded_bytes:finalDecoded.audio_bytes,media_time:finalDecoded.time,audio_aus:preview.audio_aus,plays:counted.plays,containers,...(dtxGap?{dtx_gap:dtxGap,dtx_recovery:'explicit_test_seek'}:{})});
  }
  assert.equal(errors.length,0,JSON.stringify(errors));assert.equal(providerCalls.length,4);console.log(JSON.stringify({result:'PASS',results,model_calls:providerCalls.length}));
 }catch(error){console.error(logs);throw error;}
 finally{ws?.close();await stop(fixture);await stop(chrome);await stop(app);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
