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
 assert.ok(binary&&fixtureBinary,'STREAMANALYZER_TEST_SERVER and STREAMANALYZER_RTSP_STALL_FIXTURE are required');
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-rtsp-stall-browser-'));
 const providerCalls=[],providerErrors=[];
 const provider=http.createServer(async(req,res)=>{
  try {
   let body='';for await(const chunk of req)body+=chunk;
   providerCalls.push(body);
   const request=JSON.parse(body),input=JSON.parse(request.messages.find(message=>message.role==='user').content);
   for(const secret of ['rtsp://','source_ip','127.0.0.1','data:image/'])assert.ok(!body.includes(secret),secret);
   for(const item of input.stall_investigations||[]) {
    assert.equal(item.image_pixels_provided,false);
    if(item.rtsp_media){assert.equal(item.media_alignment,'receiver_rtp_estimate');assert.equal(item.rtsp_media.source_artifact.sha256.length,64);}
   }
   res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify({summary:'已核对 RTSP 离线媒体时序证据',hypotheses:[]})}}]}));
  } catch(error) {providerErrors.push(error.message);res.writeHead(500,{'Content-Type':'application/json'});res.end(JSON.stringify({error:'fixture model request assertion failed'}));}
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'rtsp-stall-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:['ignore','pipe','pipe']});
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
  const results=[];
  const getDetail=id=>api('GET','/api/diagnostics/runs/'+id);
  const bytesAt=async url=>Buffer.from(await(await fetch(base+url)).arrayBuffer());
  const importZIP=async(buffer,label)=>{
   const seen=new Set((await api('GET','/api/diagnostics/runs')).map(run=>run.id));
   const handle=await cdp('DOM.getDocument'),input=await cdp('DOM.querySelector',{nodeId:handle.root.nodeId,selector:'#archive'});
   const file=path.join(directory,label+'.zip');await fs.writeFile(file,buffer);await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[file]});await evaluate(`document.getElementById('import').requestSubmit()`);
   return until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(run=>run.mode==='replay'&&!seen.has(run.id));});
  };
  const investigate=async(id,stallIndex)=>{
   await api('POST',`/api/diagnostics/runs/${id}/stalls`,{stall_index:stallIndex});
   return until(async()=>{const detail=await getDetail(id);const item=detail.report?.stall_investigations?.[0];return item&&!['queued','running'].includes(item.status)&&item.model_state!=='pending'?detail:false;},65);
  };
  const verifyAssets=async(detail)=>{
   const item=detail.report.stall_investigations[0];assert.ok(['ready','partial'].includes(item.media.status),JSON.stringify(item.media));if(item.media.status==='partial')assert.ok(item.media.reason,'partial media requires an explicit reason');assert.equal(item.media.alignment,'receiver_rtp_estimate');
   assert.equal(item.media.assets.length,4,JSON.stringify(item.media));assert.ok(item.media.rtsp);assert.ok(item.media.rtsp.anchor_current_ms>=0);assert.ok(item.media.rtsp.video_access_units>20&&item.media.rtsp.audio_access_units>20);
   const assets={};
   for(const asset of item.media.assets){
    const name=asset.name.split('/').at(-1),url=`/api/diagnostics/runs/${detail.id}/stall-assets/${item.id}/${name}`,data=await bytesAt(url);
    assert.equal(data.length,asset.bytes);assert.equal(createHash('sha256').update(data).digest('hex'),asset.sha256);assets[name]=data;
    if(asset.kind==='screenshot'){
     const dimensions=await evaluate(`new Promise((resolve,reject)=>{const image=new Image();image.onload=()=>resolve([image.naturalWidth,image.naturalHeight]);image.onerror=()=>reject(Error('JPEG decode failed'));image.src=${JSON.stringify(base+url)};})`);
     assert.ok(dimensions[0]>=160&&dimensions[1]>=90,'screenshot is not a decoded source picture');
    }else{
     await evaluate(`window.clipProbe?.remove();window.clipProbe=document.createElement('video');clipProbe.muted=true;clipProbe.autoplay=true;clipProbe.playsInline=true;clipProbe.src=${JSON.stringify(base+url)};document.body.append(clipProbe);clipProbe.play().catch(()=>{});`);
     const decoded=await until(()=>evaluate(`clipProbe.readyState>=2&&clipProbe.videoWidth>0&&clipProbe.currentTime>0.2?{width:clipProbe.videoWidth,time:clipProbe.currentTime,frames:clipProbe.getVideoPlaybackQuality?.().totalVideoFrames,audio_bytes:clipProbe.webkitAudioDecodedByteCount}:false`),12);
     assert.ok(decoded.width>=160&&decoded.frames>0&&decoded.audio_bytes>0,'derived MP4 must decode both source video and audio: '+JSON.stringify(decoded));await evaluate('clipProbe.pause();clipProbe.remove();');
    }
   }
   const html=await(await fetch(base+`/api/diagnostics/runs/${detail.id}/report.html`)).text();
   assert.ok(html.includes('data:image/jpeg;base64,'),'standalone HTML must embed verified source screenshots');
   assert.ok(html.includes('接收端 RTP')&&html.includes('不能重现或证明本机画面冻结'));
   assert.ok(html.includes('clip.mp4'));return assets;
  };
  for(const transport of ['tcp','udp']){
   fixture=spawn(fixtureBinary,[],{stdio:['ignore','pipe','inherit']});
   let ready='';const source=await new Promise((resolve,reject)=>{fixture.once('error',reject);fixture.once('exit',code=>reject(new Error('fixture exited '+code)));fixture.stdout.on('data',chunk=>{ready+=chunk;const end=ready.indexOf('\n');if(end>=0){try{resolve(JSON.parse(ready.slice(0,end)));}catch(error){reject(error);}}});});
   await evaluate(`document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('rtsp-transport').value=${JSON.stringify(transport)};document.getElementById('rtsp-transport').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(source.url)};document.getElementById('seconds').value='12';document.getElementById('analyze-playback').checked=true;document.getElementById('sample').requestSubmit()`);
   const run=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='sample'&&!results.some(done=>done.id===item.id));});
   await until(()=>evaluate(`(()=>{const v=document.getElementById('sample-player');return v.videoWidth>0&&v.currentTime>0.25;})()`),12);
   const detail=await until(async()=>{const value=await getDetail(run.id);return value.report?.player?.state==='complete'?value:false;},22);
   assert.ok(detail.report.player.first_screen_ms!=null);assert.equal(detail.report.player.source,'same_capture');
   const stall=detail.report.player.stalls.findIndex(item=>item.duration_ms>=2000);assert.ok(stall>=0,'real source pause did not produce a browser buffering stall: '+JSON.stringify(detail.report.player));
   const counted=await(await fetch(source.status_url)).json();assert.equal(counted.plays,1);assert.equal(counted[transport],1);
   const raw=await bytesAt(`/api/diagnostics/runs/${run.id}/dump`),originalBundle=await bytesAt(`/api/diagnostics/runs/${run.id}/evidence.zip`);
   await stop(fixture);fixture=undefined; // Every investigation below must work with the source offline.
   const original=await investigate(run.id,stall+1);const originalAssets=await verifyAssets(original);
   const alignment=original.report.stall_investigations[0].media.rtsp;
   assert.ok(Math.abs(alignment.anchor_sample_offset_ms-alignment.anchor_current_ms)>=50,'fixture must distinguish RTP arrival from player media time');
   assert.ok(['complete','unconfigured'].includes(original.report.stall_investigations[0].model_state));
   const imported=await importZIP(originalBundle,transport+'-native');const offline=await investigate(imported.id,stall+1);const offlineAssets=await verifyAssets(offline);
   assert.deepEqual(offline.report.stall_investigations[0].media,original.report.stall_investigations[0].media);
   for(const name of Object.keys(originalAssets))assert.ok(originalAssets[name].equals(offlineAssets[name]),'offline reconstruction changed '+name);
   const withCase=await bytesAt(`/api/diagnostics/runs/${imported.id}/evidence.zip`);const importedCase=await importZIP(withCase,transport+'-case');const replay=await getDetail(importedCase.id);
   assert.deepEqual(replay.report,offline.report);assert.ok(raw.equals(await bytesAt(`/api/diagnostics/runs/${importedCase.id}/dump`)));
   const importedAssets=await verifyAssets(replay);for(const name of Object.keys(offlineAssets))assert.ok(offlineAssets[name].equals(importedAssets[name]));
   await until(()=>evaluate(`document.getElementById('stall-investigation-results').textContent.includes('接收端 RTP')`));
   assert.equal(await evaluate(`new Set(Array.from(document.querySelectorAll('[id]')).map(node=>node.id)).size===document.querySelectorAll('[id]').length`),true,'duplicate case evidence IDs');
   results.push({id:run.id,transport,stall:detail.report.player.stalls[stall],first_screen_ms:detail.report.player.first_screen_ms,plays:counted.plays,alignment,assets:Object.keys(originalAssets),replay:importedCase.id});
  }
  assert.equal(errors.length,0,JSON.stringify(errors));assert.equal(providerErrors.length,0,JSON.stringify(providerErrors));assert.ok(providerCalls.length>=4);assert.ok(providerCalls.some(body=>JSON.parse(body).messages.some(message=>(message.content||'').includes('"image_pixels_provided":false'))));
  console.log(JSON.stringify({result:'PASS',results,model_calls:providerCalls.length}));
 }catch(error){console.error(logs);throw error;}
 finally{ws?.close();await stop(fixture);await stop(chrome);await stop(app);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
