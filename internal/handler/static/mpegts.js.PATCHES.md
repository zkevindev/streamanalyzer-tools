The bundled `mpegts.js` retains its upstream license headers.

StreamAnalyzer changes the Enhanced FLV Opus path to read RFC 6716 packet
framing and duration instead of assuming that every encoded packet lasts
20 ms. The MP4 remux path keeps each packet's duration and RTP-derived DTS,
including DTX and missing-packet gaps; it does not repeat an encoded packet
to fill a gap. A fractional carry handles FLV's integer-millisecond timestamps
for consecutive 2.5 ms packets. The legacy AAC/MP3 and MPEG-TS paths retain
their existing behavior.

For browsers such as WKWebView that accept Opus in WebM MSE but reject Opus
in MP4 MSE, the remuxer selects the isolated `opus-webm.js` helper. It keeps
the original OpusHead and encoded packet payloads, uses the same receiver DTS
with FLV's integer-millisecond quantisation, and retains intrinsic TOC duration
and gaps. WebM uses SimpleBlock and the default 1 ms timecode scale because
WKWebView mishandles otherwise valid nondefault scales or explicit audio
BlockDuration values. Browsers supporting Opus
in MP4 continue through the MP4 path. Neither path encodes audio or starts a
second pull.

Run `node internal/handler/testdata/opus_duration.cjs` to verify the shipped
demuxer and MP4 `trun`/`tfdt` output, supported duration variants, malformed
framing, and gaps. The test exposes webpack internals only in its isolated VM;
the production bundle adds no diagnostic hooks.
