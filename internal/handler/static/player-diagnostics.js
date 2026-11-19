'use strict';

(() => {
    const sampleIntervalMS = 250;
    const maxElapsedMS = 330000;
    const maxCurrentMS = 1000000000000;
    const maxEvents = 3000;
    const maxSamples = 1401;
    const maxFrameCounter = 1000000000;
    const maxSchedulingIntervals = 256;
    const minLongTaskMS = 50;
    const minTimerDelayMS = 100;
    const maxPresentationGaps = 128;
    const minPresentationGapMS = 1000;
    const maxFramePeriodMS = 5000;
    const minCadenceSamples = 3;
    const maxCadenceSamples = 9;
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
                socket.onclose = ({code, reason}) => {
                    this.socket = null;
                    // Queue overflow and failed subscription are transport errors, not capture EOF.
                    if (code !== socketNormalClose) {
                        this.fail(code, reason);
                        return;
                    }
                    this._status = LoaderStatus.kComplete;
                    this.onComplete(0, this.received - 1);
                };
            }

            fail(code, reason = '') {
                this._status = LoaderStatus.kError;
                this.onError(LoaderErrors.EXCEPTION, {code, preview: reason.startsWith('rtsp_preview:') ? (code === 1003 ? 'preview_unsupported' : code === 1013 ? 'preview_queue_limit' : 'preview_failed') : '', msg: 'Capture WebSocket closed unexpectedly'});
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
            this.scheduling = null;
            this.schedulingObserver = null;
            this.schedulingStopped = false;
            this.longTaskKeys = new Set();
            this.nextSampleDeadline = null;
            this.presentation = null;
            this.presentationStopped = false;
            this.presentationRegistered = false;
            this.presentationPublished = false;
            this.presentationMediaMS = null;
            this.presentationCadence = [];
        }

        elapsed() {
            return Math.min(maxElapsedMS, Math.max(0, Math.round(performance.now() - this.startClock)));
        }

        event(kind, detail = {}) {
            if (!this.active) return;
            if (['pause', 'seeking', 'ended', 'error', 'autoplay_blocked', 'unsupported', 'hidden', 'stop'].includes(kind)) {
                this.framePlaybackActive = false;
                this.resetFrameBaseline();
                this.resetSchedulingBaseline();
            } else if (kind === 'playing') {
                this.framePlaybackActive = !this.measurementEnded && !document.hidden;
                this.resetSchedulingBaseline();
                if (this.scheduling && this.framePlaybackActive && this.hasPlayed &&
                    !this.video.paused && !this.video.seeking && !this.video.ended && !this.video.error) {
                    this.nextSampleDeadline = this.elapsed() + sampleIntervalMS;
                }
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
            // Keep the earlier same-video cadence, but never measure a media-time
            // delta or a callback interval across pause, seek, background, or EOF.
            this.presentationMediaMS = null;
        }

        observePresentation() {
            if (this.presentation) return;
            const at = this.elapsed();
            this.presentation = {
                support: typeof this.video.requestVideoFrameCallback === 'function' ? 'supported' : 'unsupported',
                observation_start_ms: at,
                observation_end_ms: at,
                gaps: [],
                dropped_gaps: 0,
            };
        }

        // These are closed callback intervals, not proof of a frozen image.
        recordPresentationFrame(at, metadata) {
            const presentation = this.presentation;
            if (!presentation || presentation.support !== 'supported' || this.presentationStopped) return;
            if (this.activeFrameElapsed !== null && this.presentationCadence.length >= minCadenceSamples) {
                // Use only earlier media-time deltas: this frame's possible outlier
                // must not redefine the expectation for the interval just observed.
                const cadence = this.presentationCadence.slice().sort((a, b) => a - b);
                const middle = Math.floor(cadence.length / 2);
                const median = cadence.length % 2 ? cadence[middle] : (cadence[middle - 1] + cadence[middle]) / 2;
                const expected = Math.max(1, Math.round(median));
                if (at - this.activeFrameElapsed >= Math.max(minPresentationGapMS, 3 * expected)) {
                    if (presentation.gaps.length < maxPresentationGaps) {
                        presentation.gaps.push({start_ms: this.activeFrameElapsed, end_ms: at, expected_frame_ms: expected});
                    } else presentation.dropped_gaps = Math.min(maxFrameCounter, presentation.dropped_gaps + 1);
                }
            }
            const time = metadata?.mediaTime;
            const current = Number.isFinite(time) && time >= 0 && time <= maxCurrentMS / 1000 ? time * 1000 : null;
            if (this.activeFrameElapsed !== null && current !== null && this.presentationMediaMS !== null) {
                const delta = current - this.presentationMediaMS;
                if (delta > 0 && delta <= maxFramePeriodMS) {
                    this.presentationCadence.push(delta);
                    if (this.presentationCadence.length > maxCadenceSamples) this.presentationCadence.shift();
                    if (presentation.ready_at_ms === undefined && this.presentationCadence.length >= minCadenceSamples) {
                        presentation.ready_at_ms = at;
                    }
                }
            }
            this.presentationMediaMS = current;
        }

        presentationSnapshot() {
            const presentation = this.presentation;
            if (!presentation) {
                const at = this.elapsed();
                return {support: 'unsupported', observation_start_ms: at, observation_end_ms: at, gaps: [], dropped_gaps: 0};
            }
            if (!this.presentationStopped) presentation.observation_end_ms = Math.max(presentation.observation_end_ms, this.elapsed());
            this.presentationPublished = true;
            return {...presentation, gaps: presentation.gaps.map((gap) => ({...gap}))};
        }

        stopPresentation() {
            if (this.presentationStopped) return;
            if (this.presentation) this.presentation.observation_end_ms = Math.max(this.presentation.observation_end_ms, this.elapsed());
            this.presentationStopped = true;
        }

        resetSchedulingBaseline() {
            this.nextSampleDeadline = null;
        }

        observeScheduling() {
            if (this.scheduling) return;
            const at = this.elapsed();
            this.scheduling = {
                longtask_support: 'unsupported',
                observation_start_ms: at,
                observation_end_ms: at,
                long_tasks: [],
                timer_delays: [],
                dropped_long_tasks: 0,
                dropped_timer_delays: 0,
            };
            let observer;
            try {
                const Observer = globalThis.PerformanceObserver;
                if (!Observer?.supportedEntryTypes?.includes('longtask')) return;
                observer = new Observer((list) => {
                    this.recordLongTasks(list.getEntries());
                });
                observer.observe({type: 'longtask', buffered: true});
                this.schedulingObserver = observer;
                this.scheduling.longtask_support = 'supported';
            } catch {
                observer?.disconnect();
                // The timer observation remains available in WebViews without Long Tasks.
            }
        }

        recordLongTasks(entries) {
            const scheduling = this.scheduling;
            if (!this.active || this.schedulingStopped || !scheduling || scheduling.longtask_support !== 'supported') return;
            const end = this.elapsed();
            const intervals = [];
            for (const entry of entries) {
                if (!Number.isFinite(entry.startTime) || entry.startTime < 0 || !Number.isFinite(entry.duration) || entry.duration < minLongTaskMS) continue;
                const start = Math.max(scheduling.observation_start_ms, Math.round(entry.startTime - this.startClock));
                const finish = Math.round(entry.startTime + entry.duration - this.startClock);
                if (finish > end || finish - start < minLongTaskMS) continue;
                intervals.push({start_ms: start, end_ms: finish});
            }
            intervals.sort((a, b) => a.start_ms - b.start_ms || a.end_ms - b.end_ms);
            for (const interval of intervals) {
                const key = `${interval.start_ms}:${interval.end_ms}`;
                if (this.longTaskKeys.has(key)) continue;
                // Native long tasks are nonoverlapping and >= 50 ms: the observed
                // time window bounds this set even after the saved interval cap.
                this.longTaskKeys.add(key);
                const previous = scheduling.long_tasks.at(-1);
                if (scheduling.long_tasks.length >= maxSchedulingIntervals || (previous && interval.start_ms < previous.end_ms)) {
                    scheduling.dropped_long_tasks = Math.min(maxFrameCounter, scheduling.dropped_long_tasks + 1);
                    continue;
                }
                scheduling.long_tasks.push(interval);
            }
        }

        drainLongTasks() {
            if (!this.schedulingObserver || this.schedulingStopped) return;
            this.recordLongTasks(this.schedulingObserver.takeRecords());
        }

        sampleTick() {
            const scheduling = this.scheduling;
            const video = this.video;
            if (scheduling && this.active && !this.schedulingStopped && this.framePlaybackActive &&
                this.hasPlayed && !video.paused && !video.seeking && !video.ended && !video.error && !document.hidden) {
                const at = this.elapsed();
                if (this.nextSampleDeadline !== null && at - this.nextSampleDeadline >= minTimerDelayMS) {
                    if (scheduling.timer_delays.length < maxSchedulingIntervals) {
                        scheduling.timer_delays.push({start_ms: this.nextSampleDeadline, end_ms: at});
                    } else scheduling.dropped_timer_delays = Math.min(maxFrameCounter, scheduling.dropped_timer_delays + 1);
                }
                // Reset from the actual tick; one delayed tick must not create catch-up gaps.
                this.nextSampleDeadline = at + sampleIntervalMS;
            } else this.resetSchedulingBaseline();
            this.sample();
        }

        schedulingSnapshot() {
            const scheduling = this.scheduling;
            if (!scheduling) {
                const at = this.elapsed();
                return {longtask_support: 'unsupported', observation_start_ms: at, observation_end_ms: at,
                    long_tasks: [], timer_delays: [], dropped_long_tasks: 0, dropped_timer_delays: 0};
            }
            if (!this.schedulingStopped) scheduling.observation_end_ms = Math.max(scheduling.observation_end_ms, this.elapsed());
            return {...scheduling,
                long_tasks: scheduling.long_tasks.map((interval) => ({...interval})),
                timer_delays: scheduling.timer_delays.map((interval) => ({...interval})),
            };
        }

        stopScheduling() {
            if (this.schedulingStopped) return;
            this.drainLongTasks();
            if (this.scheduling) this.scheduling.observation_end_ms = Math.max(this.scheduling.observation_end_ms, this.elapsed());
            this.schedulingStopped = true;
            this.schedulingObserver?.disconnect();
            this.schedulingObserver = null;
            this.resetSchedulingBaseline();
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
                            this.recordPresentationFrame(at, metadata);
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
                this.presentationRegistered = true;
                return true;
            } catch {
                this.frameCallback = null;
                this.lastFrameElapsed = null;
                this.presentedFrames = null;
                if (this.presentation && !this.presentationRegistered && !this.presentationPublished && !this.firstFrame) this.presentation.support = 'unsupported';
                else if (this.presentation) this.stopPresentation();
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
                    if (type !== 'waiting') {
                        this.resetFrameBaseline();
                        this.resetSchedulingBaseline();
                    }
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
                this.resetSchedulingBaseline();
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
            this.observeScheduling();
            this.observePresentation();
            this.observeVideo();
            this.event('start');
            if (!this.active) return;
            if (document.hidden) this.event('hidden');
            if (!this.active) return;
            this.sample();
            this.timer = setInterval(() => this.sampleTick(), sampleIntervalMS);
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
            const Hls = globalThis.Hls;
            const recovery = this.options.hlsRecovery && Hls?.isSupported();
            const abr = this.options.hlsABR && Hls?.isSupported();
            if (!recovery && !abr && !this.options.hlsLowLatency && (video.canPlayType('application/vnd.apple.mpegurl') || video.canPlayType('application/x-mpegURL'))) {
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
            if (!Hls?.isSupported()) {
                this.event('unsupported', {code: 'unsupported'});
                return;
            }
            const config = {enableWorker: false};
            if (recovery) {
                const policy = Hls.DefaultConfig.fragLoadPolicy.default;
                config.fragLoadPolicy = {default: {
                    ...policy,
                    errorRetry: {
                        ...policy.errorRetry,
                        shouldRetry(_retry, count, _timeout, response, defaultRetry) {
                            return defaultRetry || (count === 0 && [0, 200, 404].includes(response?.code));
                        },
                    },
                }};
            }
            if (this.options.hlsLowLatency) config.lowLatencyMode = true;
            const player = new Hls(config);
            this.hlsPlayer = player;
            this.releaseMedia = () => {
                this.hlsPlayer = null;
                player.destroy();
            };
            if (abr) {
                player.on(Hls.Events.MANIFEST_PARSED, () => {
                    this.options.onQualities?.(player.levels, level => { player.currentLevel = level; });
                });
                player.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
                    this.options.onQualityPlaying?.(player.levels[data.level]);
                });
            }
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

        hlsLatencyMS() {
            const latency = this.hlsPlayer?.latency;
            return Number.isFinite(latency) && latency > 0 ? Math.round(latency * 1000) : null;
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
                enableWorker: false,
                enableWorkerForMSE: false,
            });
            this.releaseMedia = () => player.destroy();
            player.on(mpegts.Events.ERROR, (type, detail, info) => {
                if (type === mpegts.ErrorTypes.NETWORK_ERROR) {
                    const preview = ['preview_unsupported', 'preview_queue_limit', 'preview_failed'].includes(info?.preview) ? info.preview : '';
                    this.event(preview === 'preview_unsupported' ? 'unsupported' : 'error', {code: preview || (info?.code === socketUnavailable ? 'no_stream' : 'network_error')});
                } else if (detail === mpegts.ErrorDetails.MEDIA_CODEC_UNSUPPORTED) {
                    this.event('unsupported', {code: 'unsupported'});
                } else {
                    this.event('error', {code: 'media_error'});
                }
            });
            if (mpegts.Events.MEDIA_INFO) player.on(mpegts.Events.MEDIA_INFO, (info) => {
                const source = globalThis.MediaSource || globalThis.ManagedMediaSource;
                // A negative capability check is conclusive. A positive one
                // still requires the actual first-frame/playback observations.
                const unsupportedVideo = /^(hvc1|hev1)\./.test(info?.videoCodec || '') && source?.isTypeSupported && !source.isTypeSupported(`video/mp4; codecs="${info.videoCodec}"`);
                const opus = (info?.audioCodec || '').toLowerCase() === 'opus';
                const opusMP4 = source?.isTypeSupported?.('audio/mp4; codecs="opus"');
                const opusWebM = globalThis.StreamAnalyzerOpusWebM && source?.isTypeSupported?.('audio/webm; codecs="opus"');
                if (unsupportedVideo || (opus && source?.isTypeSupported && !opusMP4 && !opusWebM)) {
                    this.event('unsupported', {code: 'browser_codec_unsupported'});
                    player.unload();
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
            this.drainLongTasks();
            const scheduling = this.schedulingSnapshot();
            const presentation = this.presentationSnapshot();
            return {
                version: 4,
                revision: ++this.revision,
                started_at: this.startedAt.toISOString(),
                elapsed_ms: this.elapsed(),
                complete,
                engine: this.engine,
                source: this.source,
                events: this.events.slice(),
                samples: this.samples.slice(),
                scheduling,
                presentation,
            };
        }

        cutoff() {
            if (!this.active || this.measurementEnded) return;
            this.stopScheduling();
            this.stopPresentation();
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
