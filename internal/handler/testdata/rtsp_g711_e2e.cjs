// Build ./cmd/server and ./internal/diagnostic/testdata/g711-fixture first.
// Set STREAMANALYZER_TEST_SERVER and STREAMANALYZER_G711_FIXTURE to binaries.
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
 const binary=process.env.STREAMANALYZER_TEST_SERVER,fixtureBinary=process.env.STREAMANALYZER_G711_FIXTURE;
 assert.ok(binary&&fixtureBinary,'STREAMANALYZER_TEST_SERVER and STREAMANALYZER_G711_FIXTURE are required');
 const directory=await fs.mkdtemp(path.join(os.tmpdir(),'streamanalyzer-g711-browser-'));
 const providerCalls=[];
 const provider=http.createServer(async(req,res)=>{
  let body='';for await(const chunk of req)body+=chunk;
  providerCalls.push(body);
  const messages=JSON.parse(body).messages.map(message=>message.content||'').join('\n');
  assert.ok((messages.includes('"audio_codec":"pcma"')||messages.includes('"audio_codec":"pcmu"'))&&messages.includes('"audio_output_codec":"aac"')&&messages.includes('g711_to_aac')&&messages.includes('"encoder":"ffmpeg"'));
  for(const secret of ['rtsp://','source_ip','127.0.0.1'])assert.ok(!body.includes(secret),secret);
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message:{content:JSON.stringify({summary:'已核对 G711 原始接收与本机预览证据',hypotheses:[]})}}]}));
 });provider.listen(0,'127.0.0.1');await once(provider,'listening');
 const reservation=http.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');const port=reservation.address().port;await new Promise(resolve=>reservation.close(resolve));
 const base=`http://127.0.0.1:${port}`;
 await fs.writeFile(path.join(directory,'config.yaml'),`server:\n  addr: "127.0.0.1:${port}"\n  data_dir: ${JSON.stringify(path.join(directory,'data'))}\n`);
 const app=spawn(binary,['-config',path.join(directory,'config.yaml')],{env:{...process.env,STREAMANALYZER_MODEL_PROVIDER:'openai-compatible',STREAMANALYZER_MODEL_BASE_URL:`http://127.0.0.1:${provider.address().port}/v1`,STREAMANALYZER_MODEL_NAME:'g711-browser-fixture',STREAMANALYZER_MODEL_API_KEY:'test-key'},stdio:['ignore','pipe','pipe']});
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
  for(const transport of ['tcp','udp'])for(const codec of ['pcma','pcmu']){
   fixture=spawn(fixtureBinary,['-codec',codec],{stdio:['ignore','pipe','inherit']});
   let ready='';const source=await new Promise((resolve,reject)=>{fixture.once('error',reject);fixture.once('exit',code=>reject(new Error('fixture exited '+code)));fixture.stdout.on('data',chunk=>{ready+=chunk;const end=ready.indexOf('\n');if(end>=0){try{resolve(JSON.parse(ready.slice(0,end)));}catch(error){reject(error);}}});});
   await evaluate(`window.g711Observed={audio_bytes:0,time:0};clearInterval(window.g711ObservationTimer);window.g711ObservationTimer=setInterval(()=>{const video=document.getElementById('sample-player');g711Observed.audio_bytes=Math.max(g711Observed.audio_bytes,Number(video.webkitAudioDecodedByteCount)||0);g711Observed.time=Math.max(g711Observed.time,video.currentTime||0);},50);document.getElementById('protocol').value='rtsp';document.getElementById('protocol').dispatchEvent(new Event('change'));document.getElementById('rtsp-transport').value=${JSON.stringify(transport)};document.getElementById('rtsp-transport').dispatchEvent(new Event('change'));document.getElementById('url').value=${JSON.stringify(source.url)};document.getElementById('seconds').value='6';document.getElementById('analyze-playback').checked=true;document.getElementById('auto-investigate').checked=false;document.getElementById('sample-player').muted=false;document.getElementById('sample').requestSubmit()`);
   const run=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='sample'&&!results.some(done=>done.id===item.id));});
   const decoded=await until(()=>evaluate(`g711Observed.audio_bytes>0&&g711Observed.time>0.25?g711Observed:false`),12);
   const detail=await until(async()=>{const value=await api('GET','/api/diagnostics/runs/'+run.id);return value.report?.player?.state==='complete'?value:false;},15);
   const finalDecoded=await evaluate('g711Observed');
   assert.ok(finalDecoded.audio_bytes>=20000&&finalDecoded.audio_bytes>decoded.audio_bytes+5000&&finalDecoded.time>2,'audio decoding did not continue through the full fixture: '+JSON.stringify({transport,codec,first:decoded,final:finalDecoded,preview:detail.report.rtsp.preview,player:detail.report.player.state}));
   const preview=detail.report.rtsp.preview;
   assert.equal(preview.version,2);assert.ok(preview.codec_capabilities_version>=3);assert.equal(preview.audio_codec,codec);assert.equal(preview.audio_output_codec,'aac');assert.equal(preview.audio_transform,'g711_to_aac');assert.equal(preview.state,'ready');assert.ok(preview.audio_aus>20&&preview.video_aus>10);assert.equal(preview.packet_errors,0);
   assert.ok(detail.report.player.first_screen_ms!=null);assert.equal(detail.report.player.source,'same_capture');assert.equal(detail.report.rtsp.transport,transport==='tcp'?'tcp_interleaved':'udp');
   const conversion=preview.g711;assert.ok(conversion);assert.equal(conversion.encoder,'ffmpeg');assert.equal(conversion.state,'ready');assert.equal(conversion.input_sample_rate,8000);assert.equal(conversion.output_sample_rate,48000);assert.equal(conversion.channels,1);assert.ok(conversion.input_packets>100&&conversion.input_samples>16000&&conversion.output_aus>20&&conversion.output_samples>48000);assert.equal(conversion.output_aus,preview.audio_aus);assert.equal(conversion.inserted_silence_samples,0);assert.equal(conversion.dropped_packets,0);assert.equal(conversion.packet_errors,0);assert.equal(conversion.encoder_priming_samples,1024);assert.ok(conversion.tool_check_ms>=0&&conversion.startup_ms>=0&&conversion.drain_ms>=0);
   await until(()=>evaluate(`(()=>{const text=document.getElementById('rtsp-preview-metrics').textContent;return text.includes('G.711 转码状态')&&text.includes('本机 G.711 → AAC 转码')&&text.includes('编码预延迟样本')&&text.includes('${codec}');})()`));
   const ui=await evaluate(`Object.fromEntries(Array.from(document.querySelectorAll('#rtsp-preview-metrics div')).map(row=>[row.querySelector('dt').textContent,row.querySelector('dd').textContent]))`);assert.equal(ui['G.711 转码状态'],'已输出 AAC');assert.equal(ui['编码预延迟样本'],'1024');assert.equal(ui['输入样本'],String(conversion.input_samples));assert.equal(ui['输出 AAC 访问单元'],String(conversion.output_aus));
   const counted=await(await fetch(source.status_url)).json();assert.equal(counted.plays,1,'preview opened a second RTSP pull');assert.equal(counted[transport],1);
   await until(()=>evaluate(`!document.getElementById('explain').disabled`));await evaluate(`document.getElementById('explain').click()`);await until(()=>evaluate(`document.getElementById('explanation').textContent.includes('已核对 G711')`));
   const raw=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/dump`)).arrayBuffer());
   const html=await(await fetch(base+`/api/diagnostics/runs/${run.id}/report.html`)).text();assert.ok(html.includes(codec)&&html.includes('AAC')&&html.includes('FFmpeg')&&html.includes('本机 G.711 → AAC 转码')&&html.includes('编码预延迟样本'));
   const bundle=Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${run.id}/evidence.zip`)).arrayBuffer());
   await stop(fixture);fixture=undefined;
   const handle=await cdp('DOM.getDocument'),input=await cdp('DOM.querySelector',{nodeId:handle.root.nodeId,selector:'#archive'});
   const bundlePath=path.join(directory,`${transport}-${codec}.zip`);await fs.writeFile(bundlePath,bundle);await cdp('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[bundlePath]});await evaluate(`document.getElementById('import').requestSubmit()`);
   const imported=await until(async()=>{const runs=await api('GET','/api/diagnostics/runs');return runs.find(item=>item.mode==='replay'&&!results.some(done=>done.replay===item.id));});
   const replay=await api('GET','/api/diagnostics/runs/'+imported.id);assert.deepEqual(replay.report,detail.report);assert.ok(raw.equals(Buffer.from(await(await fetch(base+`/api/diagnostics/runs/${imported.id}/dump`)).arrayBuffer())));
   results.push({id:run.id,replay:imported.id,transport,codec,audio_decoded_bytes:finalDecoded.audio_bytes,media_time:finalDecoded.time,audio_aus:preview.audio_aus,plays:counted.plays,conversion});
  }
  assert.equal(errors.length,0,JSON.stringify(errors));assert.equal(providerCalls.length,4);console.log(JSON.stringify({result:'PASS',results,model_calls:providerCalls.length}));
 }catch(error){console.error(logs);throw error;}
 finally{ws?.close();await stop(fixture);await stop(chrome);await stop(app);provider.close();await fs.rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(error=>{console.error(error);process.exitCode=1;});
