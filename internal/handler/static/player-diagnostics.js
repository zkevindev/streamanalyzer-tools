'use strict';

(() => {
    const sampleIntervalMS = 250;
    const maxElapsedMS = 330000;
    const maxCurrentMS = 1000000000000;
    const maxEvents = 3000;
    const maxSamples = 1401;
    const maxFrameCounter = 1000000000;
    const socketNormalClose = 1000;
    const socketUnavailable = 1008;

    function milliseconds(seconds, maximum) {
        return Number.isFinite(seconds) ? Math.min(maximum, Math.max(0, Math.round(seconds * 1000))) : 0;
    }

    function frameCounter(value) {
        return Number.isSafeInteger(value) && value >= 0 && value <= maxFrameCounter ? value : null;
    }

    function bufferedAhead(video) {
        const current = video.currentTime;
        const ranges = video.buffered;
        for (let index = 0; index < ranges.length; index++) {
            if (ranges.start(index) <= current && current <= ranges.end(index)) {
                return milliseconds(ranges.end(index) - current, maxElapsedMS);
            }
        }
        return 0;
    }

    function mediaURL(path, transport) {
        const local = new URL(window.location.href);
        if (!path || (local.protocol !== 'http:' && local.protocol !== 'https:')) {
            throw new Error('Invalid player URL');
        }
        const url = new URL(path, local);
        const socketProtocol = local.protocol === 'https:' ? 'wss:' : 'ws:';
        const isSocket = transport !== 'hls';
        if (url.host !== local.host || url.username || url.password ||
            (url.protocol !== local.protocol && !(isSocket && url.protocol === socketProtocol))) {
            throw new Error('Invalid player URL');
        }
        if (isSocket) url.protocol = socketProtocol;
        return url.href;
    }

    function captureSocketLoader(mpegts) {
        const {BaseLoader, LoaderStatus, LoaderErrors} = mpegts;

        return class CaptureSocketLoader extends BaseLoader {
            open(source) {
                let socket;
                try {
                    socket = new WebSocket(source.url);
                } catch {
                    this.fail(0);
                    return;
                }
                this.socket = socket;
                this.received = 0;
                this._status = LoaderStatus.kConnecting;
                socket.binaryType = 'arraybuffer';
                socket.onopen = () => { this._status = LoaderStatus.kBuffering; };
                socket.onmessage = ({data}) => {
                    if (!(data instanceof ArrayBuffer)) {
                        this.abort();
                        this.fail(0);
                        return;
                    }
                    const offset = this.received;
                    this.received += data.byteLength;
                    this.onDataArrival(data, offset, this.received);
                };
                socket.onclose = ({code}) => {
                    this.socket = null;
                    // Queue overflow and failed subscription are transport errors, not capture EOF.
                    if (code !== socketNormalClose) {
                        this.fail(code);
                        return;
                    }
                    this._status = LoaderStatus.kComplete;
                    this.onComplete(0, this.received - 1);
                };
            }

            fail(code) {
                this._status = LoaderStatus.kError;
                this.onError(LoaderErrors.EXCEPTION, {code, msg: 'Capture WebSocket closed unexpectedly'});
            }

            abort() {
                const socket = this.socket;
                if (!socket) return;
                this.socket = null;
                socket.onopen = null;
                socket.onmessage = null;
                socket.onclose = null;
                socket.close();
                this._status = LoaderStatus.kComplete;
            }

            destroy() {
                this.abort();
                super.destroy();
            }
        };
    }

    class PlayerDiagnostics {
        constructor(video, grant, options = {}) {
            this.video = video;
            this.grant = grant;
            this.options = options;
            this.startedAt = new Date(options.startedAt ?? Date.now());
            const previousMS = Math.max(0, Date.now() - this.startedAt.getTime());
            this.startClock = options.startedAtPerformanceMS ?? performance.now() - previousMS;
            this.engine = grant.transport === 'hls' ? 'hlsjs' : 'mpegtsjs';
            this.source = grant.source;
            this.events = [];
            this.samples = [];
            this.listeners = [];
            this.revision = 0;
            this.firstFrame = false;
            this.hasPlayed = false;
            this.frameCallback = null;
            this.presentedFrames = null;
            this.lastFrameElapsed = null;
            this.activeFrameElapsed = null;
            this.maxFrameGapMS = null;
            this.framePlaybackActive = false;
            this.active = false;
            this.canPlay = false;
            this.measurementEnded = false;
            this.finalRecording = null;
        }

        elapsed() {
            return Math.min(maxElapsedMS, Math.max(0, Math.round(performance.now() - this.startClock)));
        }

        event(kind, detail = {}) {
            if (!this.active) return;
            if (['pause', 'seeking', 'ended', 'error', 'autoplay_blocked', 'unsupported', 'hidden', 'stop'].includes(kind)) {
                this.framePlaybackActive = false;
                this.resetFrameBaseline();
            } else if (kind === 'playing') {
                this.framePlaybackActive = !this.measurementEnded && !document.hidden;
            }
            if (this.events.length >= maxEvents - (kind === 'stop' ? 0 : 1)) return;
            this.events.push({at_ms: this.elapsed(), kind, ...detail});
            this.options.onChange?.({kind, ...detail});
        }

        sample() {
            if (!this.active || this.samples.length >= maxSamples) return;
            const video = this.video;
            const at = this.elapsed();
            if (this.samples.at(-1)?.at_ms === at) return;
            const sample = {
                at_ms: at,
                current_ms: milliseconds(video.currentTime, maxCurrentMS),
                buffer_ms: bufferedAhead(video),
                ready_state: Math.min(4, Math.max(0, video.readyState)),
                paused: video.paused,
                seeking: video.seeking,
                hidden: document.hidden,
            };
            const quality = this.videoSample(at);
            if (quality) sample.video = quality;
            this.samples.push(sample);
        }

        // Browser counters describe observed video playback, not the cause of a stall.
        videoSample(at) {
            const video = this.video;
            let source = 'unavailable';
            let total = null;
            let dropped = null;
            try {
                if (typeof video.getVideoPlaybackQuality === 'function') {
                    const quality = video.getVideoPlaybackQuality();
                    const frames = frameCounter(quality?.totalVideoFrames);
                    const losses = frameCounter(quality?.droppedVideoFrames);
                    if (frames !== null && losses !== null && losses <= frames) {
                        source = 'playback_quality';
                        total = frames;
                        dropped = losses;
                    }
                }
            } catch {
                // Some embedded browsers expose unsupported quality methods.
            }
            if (source === 'unavailable') {
                try {
                    const frames = frameCounter(video.webkitDecodedFrameCount);
                    const losses = frameCounter(video.webkitDroppedFrameCount);
                    if (frames !== null && losses !== null && losses <= frames) {
                        source = 'webkit';
                        total = frames;
                        dropped = losses;
                    }
                } catch {
                    // Unsupported platform counters must not stop buffer sampling.
                }
            }
            if (total === null && this.presentedFrames === null && this.lastFrameElapsed === null && this.maxFrameGapMS === null) return null;
            const result = {quality_source: source};
            if (total !== null) result.total_frames = total;
            if (dropped !== null) result.dropped_frames = dropped;
            if (this.presentedFrames !== null) result.presented_frames = this.presentedFrames;
            if (this.lastFrameElapsed !== null) result.frame_age_ms = Math.min(maxElapsedMS, Math.max(0, at - this.lastFrameElapsed));
            if (this.maxFrameGapMS !== null) result.max_frame_gap_ms = this.maxFrameGapMS;
            return result;
        }

        resetFrameBaseline() {
            this.activeFrameElapsed = null;
        }

        observeFrame() {
            if (!this.active || (this.measurementEnded && this.firstFrame)) return false;
            try {
                this.frameCallback = this.video.requestVideoFrameCallback((_now, metadata) => {
                    this.frameCallback = null;
                    if (!this.active) return;
                    this.recordFirstFrame('video_frame_callback');
                    if (!this.active) return;
                    if (!this.measurementEnded) {
                        const at = this.elapsed();
                        this.lastFrameElapsed = at;
                        this.presentedFrames = frameCounter(metadata?.presentedFrames);
                        if (this.framePlaybackActive && !this.video.paused && !this.video.seeking && !this.video.ended && !this.video.error && !document.hidden) {
                            if (this.activeFrameElapsed !== null) {
                                const gap = Math.max(0, at - this.activeFrameElapsed);
                                this.maxFrameGapMS = Math.max(this.maxFrameGapMS ?? 0, gap);
                            }
                            this.activeFrameElapsed = at;
                        } else {
                            this.resetFrameBaseline();
                        }
                    }
                    this.observeFrame();
                });
                return true;
            } catch {
                this.frameCallback = null;
                this.lastFrameElapsed = null;
                this.presentedFrames = null;
                this.resetFrameBaseline();
                return false;
            }
        }

        listen(target, type, callback) {
            target.addEventListener(type, callback);
            this.listeners.push(() => target.removeEventListener(type, callback));
        }

        recordFirstFrame(method) {
            if (!this.active || this.firstFrame) return;
            this.firstFrame = true;
            this.event('first_frame', {method});
        }

        observeVideo() {
            const video = this.video;
            for (const type of ['waiting', 'pause', 'seeking', 'seeked', 'ended']) {
                this.listen(video, type, () => {
                    if (type !== 'waiting') this.resetFrameBaseline();
                    if (type !== 'waiting' || !this.measurementEnded) this.event(type);
                });
            }
            this.listen(video, 'playing', () => {
                this.hasPlayed = true;
                this.event('playing');
            });
            this.listen(video, 'error', () => {
                this.resetFrameBaseline();
                const code = video.error?.code;
                if (code === 4) {
                    if (!this.measurementEnded) this.event('unsupported', {code: 'unsupported'});
                } else {
                    this.event('error', {code: code === 2 ? 'network_error' : 'media_error'});
                }
            });
            this.listen(document, 'visibilitychange', () => {
                this.resetFrameBaseline();
                const hidden = document.hidden;
                this.event(hidden ? 'hidden' : 'visible');
                if (!hidden && this.hasPlayed && !video.paused && !video.seeking && video.readyState >= 3) {
                    this.event('playing');
                }
            });
            if (typeof video.requestVideoFrameCallback === 'function') {
                if (this.observeFrame()) return;
            }
            this.listen(video, 'loadeddata', () => {
                if (video.videoWidth > 0) this.recordFirstFrame('loadeddata_estimate');
            });
        }

        start() {
            if (this.active || this.finalRecording) return;
            this.active = true;
            this.observeVideo();
            this.event('start');
            if (!this.active) return;
            if (document.hidden) this.event('hidden');
            if (!this.active) return;
            this.sample();
            this.timer = setInterval(() => this.sample(), sampleIntervalMS);
            this.deadline = setTimeout(() => this.finish(), maxElapsedMS - this.elapsed());
            try {
                const transport = this.grant.transport;
                const url = mediaURL(this.grant.url, transport);
                switch (transport) {
                case 'hls':
                    this.attachHLS(url);
                    break;
                case 'flv':
                case 'mpegts':
                    this.attachMPEGTS(url, transport);
                    break;
                default:
                    this.event('unsupported', {code: 'unsupported'});
                }
            } catch {
                this.event('error', {code: 'media_error'});
            }
        }

        attachHLS(url) {
            const video = this.video;
            if (video.canPlayType('application/vnd.apple.mpegurl') || video.canPlayType('application/x-mpegURL')) {
                this.engine = 'native-hls';
                this.releaseMedia = () => {
                    video.pause();
                    video.removeAttribute('src');
                    video.load();
                };
                video.src = url;
                video.load();
                this.canPlay = true;
                this.play();
                return;
            }
            const Hls = globalThis.Hls;
            if (!Hls?.isSupported()) {
                this.event('unsupported', {code: 'unsupported'});
                return;
            }
            const player = new Hls({enableWorker: false});
            this.releaseMedia = () => player.destroy();
            player.on(Hls.Events.ERROR, (_event, data) => {
                if (!data.fatal) return;
                const code = data.type === Hls.ErrorTypes.NETWORK_ERROR ? 'network_error' : 'media_error';
                this.event('error', {code});
            });
            player.on(Hls.Events.MEDIA_ATTACHED, () => {
                if (!this.active) return;
                player.loadSource(url);
                this.canPlay = true;
                this.play();
            });
            player.attachMedia(video);
        }

        attachMPEGTS(url, transport) {
            const mpegts = globalThis.mpegts;
            if (!mpegts?.isSupported()) {
                this.event('unsupported', {code: 'unsupported'});
                return;
            }
            const player = mpegts.createPlayer({type: transport, isLive: true, url}, {
                customLoader: captureSocketLoader(mpegts),
                lazyLoad: false,
            });
            this.releaseMedia = () => player.destroy();
            player.on(mpegts.Events.ERROR, (type, detail, info) => {
                if (type === mpegts.ErrorTypes.NETWORK_ERROR) {
                    this.event('error', {code: info?.code === socketUnavailable ? 'no_stream' : 'network_error'});
                } else if (detail === mpegts.ErrorDetails.MEDIA_CODEC_UNSUPPORTED) {
                    this.event('unsupported', {code: 'unsupported'});
                } else {
                    this.event('error', {code: 'media_error'});
                }
            });
            player.on(mpegts.Events.LOADING_COMPLETE, () => this.cutoff());
            player.attachMediaElement(this.video);
            player.load();
            this.canPlay = true;
            this.play();
        }

        async play() {
            if (!this.active || !this.canPlay) return false;
            try {
                await this.video.play();
                return this.active;
            } catch (error) {
                if (error?.name === 'NotAllowedError') {
                    this.event('autoplay_blocked', {code: 'blocked'});
                } else if (error?.name === 'NotSupportedError') {
                    if (!this.measurementEnded) this.event('unsupported', {code: 'unsupported'});
                } else if (error?.name !== 'AbortError') {
                    this.event('error', {code: 'media_error'});
                }
                return false;
            }
        }

        snapshot() {
            return this.finalRecording ?? this.recording(false);
        }

        recording(complete) {
            return {
                version: 2,
                revision: ++this.revision,
                started_at: this.startedAt.toISOString(),
                elapsed_ms: this.elapsed(),
                complete,
                engine: this.engine,
                source: this.source,
                events: this.events.slice(),
                samples: this.samples.slice(),
            };
        }

        cutoff() {
            if (!this.active || this.measurementEnded) return;
            // The decoder can still present a buffered first frame after capture ends.
            this.measurementEnded = true;
            this.resetFrameBaseline();
            if (this.firstFrame && this.frameCallback !== null) {
                this.video.cancelVideoFrameCallback?.(this.frameCallback);
                this.frameCallback = null;
            }
            this.event('stop', {code: 'stopped'});
        }

        finish() {
            if (this.finalRecording) return this.finalRecording;
            this.sample();
            this.cutoff();
            if (this.finalRecording) return this.finalRecording;
            this.active = false;
            this.canPlay = false;
            this.finalRecording = this.recording(true);
            clearInterval(this.timer);
            clearTimeout(this.deadline);
            for (const disconnect of this.listeners) disconnect();
            this.listeners.length = 0;
            if (this.frameCallback !== null) {
                this.video.cancelVideoFrameCallback?.(this.frameCallback);
                this.frameCallback = null;
            }
            try {
                this.releaseMedia?.();
            } catch {
                // Engines can already be torn down after a failed attachment.
            }
            this.releaseMedia = null;
            this.grant = null;
            this.options.onFinish?.(this.finalRecording);
            return this.finalRecording;
        }
    }

    globalThis.PlayerDiagnostics = PlayerDiagnostics;
})();
