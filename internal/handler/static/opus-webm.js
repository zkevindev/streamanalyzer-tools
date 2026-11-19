'use strict';

// Native Opus packets for WebKit's WebM MSE audio SourceBuffer. Each packet
// keeps its own timestamp and duration; reception/DTX gaps are never filled.
((scope) => {
    // WebKit's MSE parser mis-times BlockGroup durations. Use the default
    // 1 ms WebM scale and SimpleBlock; Opus TOC retains exact packet duration.
    // Absolute timestamps are floored independently, so 2.5 ms input keeps
    // bounded 2/3 ms carry and reception gaps without cumulative drift.
    const scale = 1000000;
    const encoder = new TextEncoder();
    const ids = {
        ebml: [0x1a, 0x45, 0xdf, 0xa3], segment: [0x18, 0x53, 0x80, 0x67],
        info: [0x15, 0x49, 0xa9, 0x66], tracks: [0x16, 0x54, 0xae, 0x6b],
        cluster: [0x1f, 0x43, 0xb6, 0x75],
    };
    function concat(parts) {
        const size = parts.reduce((sum, part) => sum + part.byteLength, 0);
        const result = new Uint8Array(size);
        let offset = 0;
        for (const part of parts) { result.set(part, offset); offset += part.byteLength; }
        return result;
    }
    function sizeVint(value) {
        if (!Number.isSafeInteger(value) || value < 0) throw new RangeError('Invalid WebM element size');
        for (let width = 1; width <= 7; width++) {
            if (value < 2 ** (7 * width) - 1) {
                const out = new Uint8Array(width);
                let remaining = value;
                for (let index = width - 1; index >= 0; index--) { out[index] = remaining % 256; remaining = Math.floor(remaining / 256); }
                out[0] |= 1 << (8 - width);
                return out;
            }
        }
        throw new RangeError('WebM element is too large');
    }
    function uint(value) {
        if (!Number.isSafeInteger(value) || value < 0) throw new RangeError('Invalid WebM unsigned integer');
        let width = 1;
        while (value >= 2 ** (8 * width)) width++;
        const out = new Uint8Array(width);
        for (let index = width - 1; index >= 0; index--) { out[index] = value % 256; value = Math.floor(value / 256); }
        return out;
    }
    function element(id, payload) { return concat([new Uint8Array(id), sizeVint(payload.byteLength), payload]); }
    function integer(id, value) { return element(id, uint(value)); }
    function text(id, value) { return element(id, encoder.encode(value)); }
    function master(id, children) { return element(id, concat(children)); }
    function init(opusHead) {
        if (!(opusHead instanceof Uint8Array) || opusHead.byteLength !== 19 ||
            String.fromCharCode(...opusHead.subarray(0, 8)) !== 'OpusHead' || opusHead[8] !== 1 ||
            ![1, 2].includes(opusHead[9]) || opusHead[18] !== 0) throw new TypeError('Unsupported WebM OpusHead');
        const samplingFrequency = new Uint8Array(8);
        new DataView(samplingFrequency.buffer).setFloat64(0, 48000, false);
        const preSkip = opusHead[10] | opusHead[11] << 8;
        const ebml = master(ids.ebml, [integer([0x42, 0x86], 1), integer([0x42, 0xf7], 1), integer([0x42, 0xf2], 4), integer([0x42, 0xf3], 8), text([0x42, 0x82], 'webm'), integer([0x42, 0x87], 4), integer([0x42, 0x85], 2)]);
        const info = master(ids.info, [integer([0x2a, 0xd7, 0xb1], scale), text([0x4d, 0x80], 'StreamAnalyzer'), text([0x57, 0x41], 'StreamAnalyzer')]);
        const track = master([0xae], [integer([0xd7], 1), integer([0x73, 0xc5], 1), integer([0x83], 2), text([0x86], 'A_OPUS'), element([0x63, 0xa2], opusHead), integer([0x56, 0xaa], Math.round(preSkip * 1e9 / 48000)), integer([0x56, 0xbb], 80000000), master([0xe1], [element([0xb5], samplingFrequency), integer([0x9f], opusHead[9])])]);
        // An open Segment is valid for live MSE and avoids rewriting sizes as
        // later timestamped clusters arrive through the same SourceBuffer.
        return concat([ebml, new Uint8Array(ids.segment), new Uint8Array([1, 255, 255, 255, 255, 255, 255, 255]), info, master(ids.tracks, [track])]);
    }
    function cluster(samples) {
        if (!Array.isArray(samples) || samples.length > 4096) throw new RangeError('Invalid WebM sample batch');
        const clusters = [];
        let children = [], clusterStart = -1, previousDts = -1;
        const flush = () => { if (children.length) clusters.push(master(ids.cluster, [integer([0xe7], clusterStart), ...children])); children = []; };
        for (const sample of samples) {
            if (!(sample.unit instanceof Uint8Array) || sample.unit.byteLength === 0 || sample.unit.byteLength > 65535 ||
                !Number.isFinite(sample.dts) || sample.dts < 0 || sample.dts > 3600000 ||
                !Number.isFinite(sample.duration) || sample.duration <= 0 || sample.duration > 120) throw new RangeError('Invalid WebM Opus sample');
            const dts = Math.floor(sample.dts);
            if (dts < previousDts) throw new RangeError('WebM Opus timestamps regressed');
            previousDts = dts;
            if (clusterStart < 0 || dts - clusterStart > 32767) { flush(); clusterStart = dts; }
            const relative = dts - clusterStart;
            const block = concat([new Uint8Array([0x81, relative >> 8 & 255, relative & 255, 0x80]), sample.unit]);
            children.push(element([0xa3], block));
        }
        flush();
        return concat(clusters);
    }
    scope.StreamAnalyzerOpusWebM = Object.freeze({init, cluster});
})(globalThis);
