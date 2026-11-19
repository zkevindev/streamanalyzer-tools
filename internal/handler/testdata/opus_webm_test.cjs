const assert=require('node:assert/strict');
require('../static/opus-webm.js');
const mux=globalThis.StreamAnalyzerOpusWebM;
function read(data,offset=0,end=data.length){const output=[];while(offset<end){const begin=offset;let idWidth=1;for(let mask=128;!(data[offset]&mask);mask>>=1)idWidth++;const id=Buffer.from(data.subarray(offset,offset+idWidth)).toString('hex');offset+=idWidth;let width=1,mask=128;while(!(data[offset]&mask)){width++;mask>>=1;}let size=data[offset]&mask-1;let unknown=size===mask-1;for(let i=1;i<width;i++){size=size*256+data[offset+i];unknown&&=data[offset+i]===255;}offset+=width;if(unknown)size=end-offset;assert.ok(offset+size<=end,'EBML bounds');const payload=data.subarray(offset,offset+size);output.push({id,payload,begin});offset+=size;}return output;}
const uint=b=>Array.from(b).reduce((v,n)=>v*256+n,0);
const descend=(nodes,id)=>nodes.flatMap(n=>n.id===id?[n]:['18538067','1a45dfa3','1549a966','1654ae6b','ae','e1','1f43b675','a0'].includes(n.id)?descend(read(n.payload),id):[]);
const head=new Uint8Array(19);head.set(Buffer.from('OpusHead'));head[8]=1;head[9]=2;new DataView(head.buffer).setUint32(12,48000,true);
const header=mux.init(head),tree=read(header);
assert.equal(uint(descend(tree,'2ad7b1')[0].payload),1000000);
assert.ok(Buffer.from(descend(tree,'63a2')[0].payload).equals(Buffer.from(head)),'OpusHead changed');assert.equal(uint(descend(tree,'9f')[0].payload),2);assert.equal(uint(descend(tree,'56aa')[0].payload),0);
const samples=[{dts:2000,duration:2.5,unit:new Uint8Array([0xe0,1,2])},{dts:2002.5,duration:2.5,unit:new Uint8Array([0xe0,3,4])},{dts:2005,duration:2.5,unit:new Uint8Array([0xe0,5,6])},{dts:2207.5,duration:120,unit:new Uint8Array([0xfb,2,7,8])}];
const clusters=read(mux.cluster(samples));let index=0;
for(const cluster of clusters){assert.equal(cluster.id,'1f43b675');const children=read(cluster.payload),base=uint(children.find(n=>n.id==='e7').payload);for(const item of children.filter(n=>n.id==='a3')){const block=item.payload,relative=(block[1]<<8|block[2])<<16>>16,sample=samples[index++];assert.equal(block[0],0x81);assert.equal(block[3],0x80);assert.equal(base+relative,Math.floor(sample.dts));assert.ok(Buffer.from(block.subarray(4)).equals(Buffer.from(sample.unit)));}}
assert.equal(index,samples.length);assert.equal(read(mux.cluster([{dts:0,duration:20,unit:new Uint8Array([0xf8,1])},{dts:40000,duration:20,unit:new Uint8Array([0xf8,2])}])).length,2);assert.equal(mux.cluster([]).length,0);
for(const bad of [undefined,new Uint8Array(18),Object.assign(new Uint8Array(head),{9:3}),Object.assign(new Uint8Array(head),{18:1})])assert.throws(()=>mux.init(bad));
for(const bad of [{dts:-1},{dts:NaN},{duration:0},{duration:121},{unit:new Uint8Array()}])assert.throws(()=>mux.cluster([{...samples[0],...bad}]));assert.throws(()=>mux.cluster([samples[1],samples[0]]));
console.log(JSON.stringify({result:'PASS',cases:['OpusHead/channel/codec delay','2.5 ms original TOC and bounded timestamp carry','absolute timestamps and DTX gaps','long batch splits clusters','exact packet bytes','invalid input bounded']}));
