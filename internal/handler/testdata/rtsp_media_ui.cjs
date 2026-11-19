// Run with Node 22+ and Chrome; set CHROME_BIN when Chrome is not at the platform default.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const path = require('node:path');
const {spawn} = require('node:child_process');
const {once} = require('node:events');
const {setTimeout: delay} = require('node:timers/promises');
const root=path.resolve(__dirname,'../static');
const profile=path.join(require('node:os').tmpdir(),'streamanalyzer-rtsp-media-ui-'+process.pid);
const chromePath=process.env.CHROME_BIN||(process.platform==='darwin'?'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome':process.platform==='win32'?'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe':'google-chrome');
async function until(f){for(let n=0;n<100;n++){const result=await f();if(result)return result;await delay(100);}throw new Error('Timeout');}
(async()=>{
 const server=http.createServer(async(req,res)=>{
  if(req.url.startsWith('/api/')){res.setHeader('Content-Type','application/json');res.end(req.url.endsWith('/config')?'{}':'[]');return;}
  const name=req.url==='/diagnostics'?'diagnostics.html':req.url.replace(/^\/static\//,'');
  try{const body=await fs.readFile(path.join(root,name));res.setHeader('Content-Type',name.endsWith('.js')?'text/javascript':name.endsWith('.css')?'text/css':'text/html');res.end(body);}catch{res.writeHead(404).end();}
 });
 server.listen(0,'127.0.0.1');await once(server,'listening');
 const chrome=spawn(chromePath,['--headless=new','--disable-gpu','--no-first-run',`--user-data-dir=${profile}`,'--remote-debugging-port=0','about:blank'],{stdio:'ignore'});
 let ws;
 try{
  const debug=await until(()=>fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8').catch(()=>''));
  const tabs=await(await fetch(`http://127.0.0.1:${debug.split('\n')[0]}/json/list`)).json();
  ws=new WebSocket(tabs.find(tab=>tab.type==='page').webSocketDebuggerUrl);await once(ws,'open');
  const pending=new Map();let n=0;const errors=[];
  ws.addEventListener('message',({data})=>{const msg=JSON.parse(data);if(msg.id){const item=pending.get(msg.id);pending.delete(msg.id);msg.error?item.reject(msg.error):item.resolve(msg.result);}else if(msg.method==='Runtime.exceptionThrown')errors.push(msg.params);});
  const cdp=(method,params={})=>new Promise((resolve,reject)=>{pending.set(++n,{resolve,reject});ws.send(JSON.stringify({id:n,method,params}));});
  const evaluate=async expression=>{const result=await cdp('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true,replMode:true});if(result.exceptionDetails)throw new Error(JSON.stringify(result.exceptionDetails));return result.result.value;};
  await cdp('Runtime.enable');await cdp('Page.navigate',{url:`http://127.0.0.1:${server.address().port}/diagnostics`});
  await until(()=>evaluate(`typeof renderRequestTiming === 'function'`).catch(()=>false));
  const evidence={version:1,source_artifact:{bytes:0,sha256:'a'.repeat(64)},timeline_artifact:{bytes:10,sha256:'b'.repeat(64)},player_artifact:{bytes:20,sha256:'c'.repeat(64)},remux_artifact:{bytes:30,sha256:'d'.repeat(64)},player_revision:0,video_track_id:1,video_codec:'h265',audio_track_id:2,audio_codec:'pcmu',audio_transform:'g711_to_pcm',anchor_sample_offset_ms:7000,anchor_current_ms:3000,preview_origin_ms:2000,media_origin_ms:-66,duration_ms:8000,packet_count:123,video_access_units:60,audio_access_units:100,dropped_packets:0,incomplete_access_units:2,keyframes:[{dts_ms:0,pts_ms:66,arrival_ms:9000,track:1,artifact:'udp-datagrams.bin',offset:123}]};
  const item={id:'S001',stall_index:1,status:'complete',stall:{start_ms:6000,end_ms:7500,duration_ms:1500},correlation:{source:'same_capture',window_start_ms:3000,window_end_ms:10500,findings:[{id:'S001-E001',message:'核对 RTP 到达与浏览器缓冲',start_ms:6000,end_ms:7500}]},media:{status:'ready',alignment:'receiver_rtp_estimate',start_ms:0,end_ms:3200,assets:[],rtsp:evidence},model_state:'complete',explanation:{summary:'结合时序记录继续核对',hypotheses:[{claim:'源端媒体发送需核对',evidence_ids:['S001-E001'],next_check:'对照源端日志'}]}};
  await evaluate(`currentID='rtsp-media';currentReport={manifest:{protocol:'rtsp'}};window.mediaFixture=${JSON.stringify(item)};const host=document.createElement('main');host.id='rtsp-media-test';document.body.replaceChildren(host);host.append(renderInvestigation(mediaFixture));host.firstElementChild.open=true;`);
  const metrics=await evaluate(`Object.fromEntries(Array.from(document.querySelectorAll('.rtsp-media-metrics > div')).map(row=>[row.querySelector('dt').textContent,row.querySelector('dd').textContent]))`);
  assert.equal(metrics['锚点采样偏移'],'7000 ms');assert.equal(metrics['锚点播放器媒体时间'],'3000 ms');assert.equal(metrics['预览解码时钟起点'],'2000 ms');assert.equal(metrics['重组媒体起点'],'-66 ms');assert.equal(metrics['未用于重建的 RTP 包'],'0');assert.equal(metrics['播放器记录修订'],'#0');assert.equal(metrics['重建音频处理'],'G.711 解码为 PCM');
  const text=await evaluate(`document.getElementById('rtsp-media-test').textContent`);
  for(const label of ['接收端 RTP 时钟与播放器媒体时间估算','不能重现或证明本机画面冻结','该历史结果未记录截图输入方式','不会重新连接源站','也未按收包到达时间直接定位画面','重建媒体时间 0 ms','重建依据与文件指纹','0 ms66 ms9000 ms'])assert.ok(text.includes(label),label);
  await evaluate(`document.querySelector('.rtsp-media-identities').open=true`);
  for(const width of [1440,375]){await cdp('Emulation.setDeviceMetricsOverride',{width,height:1000,deviceScaleFactor:1,mobile:false});assert.equal(await evaluate(`document.documentElement.scrollWidth>innerWidth`),false,'page overflow at '+width);}
  assert.equal(await evaluate(`document.querySelector('a[href="#case-S001-E001"]').getAttribute('href')`),'#case-S001-E001');
  await evaluate(`const second=structuredClone(mediaFixture);second.id='S002';second.correlation.findings[0].id='S002-E001';second.explanation.hypotheses[0].evidence_ids=['S002-E001'];document.getElementById('rtsp-media-test').append(renderInvestigation(second));`);
  assert.equal(await evaluate(`new Set(Array.from(document.querySelectorAll('[id]')).map(node=>node.id)).size===document.querySelectorAll('[id]').length`),true);
  await evaluate(`const legacy=structuredClone(mediaFixture);delete legacy.media.rtsp;legacy.media.alignment='';document.getElementById('rtsp-media-test').replaceChildren(renderInvestigation(legacy));`);
  assert.equal(await evaluate(`document.querySelectorAll('.rtsp-media-evidence').length`),0);
  await evaluate(`mediaFixture.media.rtsp.video_track_id=0;mediaFixture.media.rtsp.audio_codec='<img src=x onerror=alert(1)>';mediaFixture.media.rtsp.keyframes=[];delete mediaFixture.media.rtsp.packet_count;document.getElementById('rtsp-media-test').replaceChildren(renderInvestigation(mediaFixture));document.querySelector('details').open=true;`);
  const audioOnly=await evaluate(`document.querySelector('.rtsp-media-metrics').textContent`);assert.ok(audioOnly.includes('源视频轨道未选择'));assert.ok(audioOnly.includes('检查 RTP 包未观测'));assert.equal(await evaluate(`document.querySelectorAll('.rtsp-media-evidence img').length`),0);
  assert.equal(errors.length,0,JSON.stringify(errors));console.log(JSON.stringify({result:'PASS',cases:['receiver clock estimate and independent origins','zero versus unknown and negative time','native input versus G711 PCM conversion','source content and model pixel boundary','keyframe RTP packet reference and media timestamps','SHA-256 identities','legacy no fabricated provenance','audio-only and inert untrusted labels','375/1440 responsive layout','case evidence anchors and unique IDs']}));
 }finally{ws?.close();chrome.kill('SIGTERM');await once(chrome,'exit').catch(()=>{});server.close();await fs.rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100});}
})().catch(err=>{console.error(err);process.exitCode=1;});
