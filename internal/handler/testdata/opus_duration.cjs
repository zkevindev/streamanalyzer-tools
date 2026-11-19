// Validate the shipped bundle's actual demux/remux output, including MP4 trun.
// No test-only runtime entry point is added to the production browser bundle.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
let source = fs.readFileSync(path.join(__dirname, '../static/mpegts.js'), 'utf8');
const entry = 'return __webpack_require__(__webpack_require__.s = "./src/index.js");';
assert.ok(source.includes(entry));
source = source.replace(entry, 'return __webpack_require__;');
const sandbox = {console, navigator: {userAgent: 'Mozilla/5.0 Chrome/130.0.0.0 Safari/537.36'}};
sandbox.window = sandbox.self = sandbox;
vm.runInNewContext(source, sandbox);
const bundle = sandbox.mpegts;
const FLV = bundle('./src/demux/flv-demuxer.js').default;
const Remuxer = bundle('./src/remux/mp4-remuxer.js').default;
const config = bundle('./src/config.js').createDefaultConfig();
function demux() {
 const parser = new FLV({hasAudioTrack: true, hasVideoTrack: false, dataOffset: 9}, config);
 parser.onError = (kind, message) => { throw new Error(kind + ': ' + message); };
 parser.onMediaInfo = () => {};
 parser.onTrackMetadata = () => {};
 parser.onDataAvailable = () => {};
 return parser;
}
const frames = [[0x80, 120], [0x88, 240], [0x90, 480], [0xf8, 960], [0x10, 1920], [0x18, 2880]];
for (const [toc, samples] of frames) assert.equal(demux()._opusPacketDuration(Uint8Array.from([toc, 1])), samples / 48);
for (const data of [[], [0xf9, 1], [0xfa], [0xfb, 0], [0xfb, 7, 1, 2, 3, 4, 5, 6, 7], [0xfb, 0x41, 2, 0]]) {
 assert.equal(demux()._opusPacketDuration(Uint8Array.from(data)), 0);
}
function run(packets) {
 const parser = demux();
 const head = Uint8Array.from([...Buffer.from('OpusHead'), 1, 2, 0, 0, 0x80, 0xbb, 0, 0, 0, 0, 0]);
 parser._parseOpusSequenceHeader(head.buffer, 0, head.length);
 assert.equal(parser._audioMetadata.refSampleDuration, null);
 for (const packet of packets) {
  const data = Uint8Array.from(packet.data);
  parser._parseOpusAudioData(data.buffer, 0, data.length, packet.at);
 }
 const remux = new Remuxer(config);
 remux._audioMeta = parser._audioMetadata;
 remux._dtsBase = 0; remux._dtsBaseInited = true;
 const segments = [];
 remux.onMediaSegment = (kind, segment) => { assert.equal(kind, 'audio'); segments.push(segment); };
 remux._remuxAudio(parser._audioTrack, true);
 remux.flushStashedSamples();
 const output = [];
 for (const segment of segments) {
  const data = Buffer.from(segment.data);
  const trun = data.indexOf(Buffer.from('trun'));
  const tfdt = data.indexOf(Buffer.from('tfdt'));
  assert.ok(trun > 0 && tfdt > 0);
  const count = data.readUInt32BE(trun + 8);
  let dts = data.readUInt32BE(tfdt + 8);
  for (let index = 0; index < count; index++) {
   const duration = data.readUInt32BE(trun + 16 + index * 16);
   const size = data.readUInt32BE(trun + 20 + index * 16);
   output.push({dts, duration, size}); dts += duration;
  }
 }
 return output;
}
const variants = run([
 {at:2000,data:[0x88,1]}, {at:2005,data:[0x90,1]}, {at:2015,data:[0xf8,0xff,0xfe]},
 {at:2035,data:[0x10,1]}, {at:2075,data:[0x18,1]}, {at:2135,data:[0xfb,6,1,2,3,4,5,6]},
]);
assert.deepEqual(variants.map(x=>[x.dts,x.duration]), [[2000,5],[2005,10],[2015,20],[2035,40],[2075,60],[2135,120]]);
const halfMs = run([0,2,5,7,10,12].map(at=>({at:2000+at,data:[0x80,1]})));
assert.deepEqual(halfMs.map(x=>[x.dts,x.duration]), [[2000,2],[2002,3],[2005,2],[2007,3],[2010,2],[2012,3]]);
const gap = run([{at:2000,data:[0xf8,0xff,0xfe]},{at:2100,data:[0xf8,0xff,0xfe]},{at:2120,data:[0xf8,0xff,0xfe]}]);
assert.deepEqual(gap.map(x=>[x.dts,x.duration]), [[2000,20],[2100,20],[2120,20]]);
assert.equal(gap.length, 3, 'DTX/loss gap must not duplicate audio to fill silence');
console.log(JSON.stringify({result:'PASS',cases:['RFC6716 framing and bounds','OpusHead dOps conversion','5/10/20/40/60/120ms encoded durations','2.5ms quantisation carry','DTX/loss gap without synthetic audio','actual MP4 trun/tfdt output']}));
