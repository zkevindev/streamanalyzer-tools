/* global echarts */
(function () {
  window.__streamAnalyzerChart = null;

  function padToLen(arr, n) {
    const out = [];
    for (let i = 0; i < n; i++) {
      out.push(arr[i] !== undefined && arr[i] !== null ? arr[i] : null);
    }
    return out;
  }

  function toTimestampSeries(arr) {
    const out = [];
    for (let i = 0; i < (arr || []).length; i++) {
      const ts = Number(arr[i]);
      if (!Number.isNaN(ts)) out.push([ts, ts]);
    }
    return out;
  }

  function calcAVSyncDiffSeries(videoDts, audioDts) {
    const v = (videoDts || []).map(Number).filter(function (x) { return !Number.isNaN(x); });
    const a = (audioDts || []).map(Number).filter(function (x) { return !Number.isNaN(x); });
    if (v.length === 0 || a.length === 0) return [];
    const out = [];
    let j = 0;
    for (let i = 0; i < v.length; i++) {
      const ts = v[i];
      while (j + 1 < a.length && Math.abs(a[j + 1] - ts) <= Math.abs(a[j] - ts)) j++;
      out.push([ts, ts - a[j]]);
    }
    return out;
  }

  function avSyncLevel(diffMs) {
    var v = Math.abs(Number(diffMs));
    if (Number.isNaN(v)) return { label: '未知', color: '#6b7280' };
    if (v <= 20) return { label: '优秀', color: '#16a34a' };
    if (v <= 50) return { label: '可接受', color: '#ca8a04' };
    if (v <= 100) return { label: '偏大', color: '#ea580c' };
    return { label: '异常', color: '#dc2626' };
  }

  function baseGrid() {
    return {
      left: '3%',
      right: '4%',
      bottom: '12%',
      top: '14%',
      containLabel: true,
    };
  }

  window.ensureMainChart = function (domId) {
    const dom = document.getElementById(domId || 'mainChart');
    if (!dom || typeof echarts === 'undefined') return null;
    if (!window.__streamAnalyzerChart) {
      window.__streamAnalyzerChart = echarts.init(dom);
      window.addEventListener('resize', function () {
        if (window.__streamAnalyzerChart) window.__streamAnalyzerChart.resize();
      });
    }
    return window.__streamAnalyzerChart;
  };

  window.disposeMainChart = function () {
    if (window.__streamAnalyzerChart) {
      window.__streamAnalyzerChart.dispose();
      window.__streamAnalyzerChart = null;
    }
  };

  /**
   * @param {string} type
   * @param {object} d - ChartData from /api/task/:id/data
   * @param {string} domId
   */
  window.renderClientChart = function (type, d, domId) {
    const chart = window.ensureMainChart(domId);
    if (!chart) return;
    if (!d || !d.frame_types || d.frame_types.length === 0) {
      chart.clear();
      chart.setOption({
        title: { text: '暂无数据', left: 'center', top: 'middle' },
      });
      return;
    }

    let option = {};

    switch (type) {
      case 'timestamp': {
        const vts = d.video_dts || [];
        const vpts = d.video_pts || [];
        const ats = d.audio_dts || [];
        option = {
          title: { text: '时间戳对齐视图 (ms)', left: 'center' },
          tooltip: { trigger: 'axis' },
          legend: { data: ['视频DTS', '视频PTS', '音频DTS'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'value', name: '时间戳(ms)', scale: true },
          yAxis: { type: 'value', name: 'ms', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '视频DTS',
              type: 'line',
              data: toTimestampSeries(vts),
              showSymbol: false,
            },
            {
              name: '视频PTS',
              type: 'line',
              data: toTimestampSeries(vpts),
              showSymbol: false,
            },
            {
              name: '音频DTS',
              type: 'line',
              data: toTimestampSeries(ats),
              showSymbol: false,
            },
          ],
        };
        break;
      }
      case 'length': {
        const vls = d.video_lens || [];
        const als = d.audio_lens || [];
        const n = Math.max(vls.length, als.length, 1);
        const x = Array.from({ length: n }, (_, i) => String(i));
        option = {
          title: { text: '数据长度 (bytes)', left: 'center' },
          tooltip: { trigger: 'axis' },
          legend: { data: ['视频', '音频'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '序号', data: x },
          yAxis: { type: 'value', name: 'bytes', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '视频',
              type: 'line',
              data: padToLen(vls, n),
              showSymbol: false,
            },
            {
              name: '音频',
              type: 'line',
              data: padToLen(als, n),
              showSymbol: false,
            },
          ],
        };
        break;
      }
      case 'iframe': {
        const raw = d.iframe_intervals || [];
        const vals = raw.filter(function (v) {
          return v > 0;
        });
        const x = vals.map(function (_, i) {
          return String(i);
        });
        option = {
          title: { text: 'I 帧间隔 (ms)', left: 'center' },
          tooltip: { trigger: 'axis' },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '序号', data: x },
          yAxis: { type: 'value', name: 'ms' },
          series: [{ name: 'I帧间隔', type: 'bar', data: vals }],
        };
        break;
      }
      case 'gop': {
        const gops = d.gop_sizes || [];
        const x = gops.map(function (_, i) {
          return String(i);
        });
        option = {
          title: { text: 'GOP 大小', left: 'center' },
          tooltip: { trigger: 'axis' },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '帧序号', data: x },
          yAxis: { type: 'value', name: '帧数' },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [{ name: 'GOP', type: 'line', data: gops, showSymbol: false }],
        };
        break;
      }
      case 'bitrate': {
        const stats = d.second_stats || [];
        const x = stats.map(function (_, i) {
          return String(i);
        });
        option = {
          title: { text: '码率 (Kbps, 相对秒)', left: 'center' },
          tooltip: { trigger: 'axis' },
          legend: { data: ['视频', '音频'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '相对秒', data: x },
          yAxis: { type: 'value', name: 'Kbps' },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '视频',
              type: 'line',
              data: stats.map(function (s) {
                return s.video_bitrate;
              }),
              showSymbol: false,
            },
            {
              name: '音频',
              type: 'line',
              data: stats.map(function (s) {
                return s.audio_bitrate;
              }),
              showSymbol: false,
            },
          ],
        };
        break;
      }
      case 'fps': {
        const stats = d.second_stats || [];
        const x = stats.map(function (_, i) {
          return String(i);
        });
        option = {
          title: { text: '每秒帧数 (相对秒)', left: 'center' },
          tooltip: { trigger: 'axis' },
          legend: { data: ['视频FPS', '音频FPS'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '相对秒', data: x },
          yAxis: { type: 'value', name: 'FPS' },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '视频FPS',
              type: 'line',
              data: stats.map(function (s) {
                return s.video_fps;
              }),
              showSymbol: false,
            },
            {
              name: '音频FPS',
              type: 'line',
              data: stats.map(function (s) {
                return s.audio_fps;
              }),
              showSymbol: false,
            },
          ],
        };
        break;
      }
      case 'avsync': {
        const series = calcAVSyncDiffSeries(d.video_dts || [], d.audio_dts || []);
        option = {
          title: { text: 'AV 同步偏差 (视频DTS-最近音频DTS, ms)', left: 'center' },
          tooltip: {
            trigger: 'axis',
            formatter: function (params) {
              if (!params || !params.length) return '';
              const p = params[0];
              const x = (p.data && p.data[0] !== undefined) ? p.data[0] : '-';
              const y = (p.data && p.data[1] !== undefined) ? p.data[1] : '-';
              const lv = avSyncLevel(y);
              return '视频DTS: ' + x + ' ms<br/>偏差: ' + y + ' ms<br/>等级: <span style="color:' + lv.color + ';">' + lv.label + '</span>';
            },
          },
          grid: baseGrid(),
          xAxis: { type: 'value', name: '视频DTS(ms)', scale: true },
          yAxis: { type: 'value', name: '偏差(ms)', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          visualMap: {
            show: true,
            type: 'piecewise',
            right: 8,
            top: 60,
            dimension: 1,
            pieces: [
              { gte: -20, lte: 20, label: '|diff|<=20 优秀', color: '#16a34a' },
              { gt: 20, lte: 50, label: '20~50 可接受', color: '#ca8a04' },
              { gte: -50, lt: -20, label: '-50~-20 可接受', color: '#ca8a04' },
              { gt: 50, lte: 100, label: '50~100 偏大', color: '#ea580c' },
              { gte: -100, lt: -50, label: '-100~-50 偏大', color: '#ea580c' },
              { gt: 100, label: '>100 异常', color: '#dc2626' },
              { lt: -100, label: '<-100 异常', color: '#dc2626' },
            ],
          },
          series: [
            {
              name: 'AV偏差',
              type: 'line',
              data: series,
              showSymbol: false,
              markLine: {
                symbol: 'none',
                lineStyle: { type: 'dashed', color: '#9ca3af' },
                label: { formatter: '{b}' },
                data: [
                  { yAxis: 0, name: '0ms' },
                  { yAxis: 20, name: '+20ms' },
                  { yAxis: -20, name: '-20ms' },
                  { yAxis: 50, name: '+50ms' },
                  { yAxis: -50, name: '-50ms' },
                  { yAxis: 100, name: '+100ms' },
                  { yAxis: -100, name: '-100ms' },
                ],
              },
            },
          ],
        };
        break;
      }
      default:
        option = {
          title: { text: '未知图表类型', left: 'center' },
        };
    }

    chart.setOption(option, true);
  };

  window.ensureHLSChart = function (domId) {
    const dom = document.getElementById(domId || 'hlsMainChart');
    if (!dom || typeof echarts === 'undefined') return null;
    if (!window.__streamAnalyzerHLSChart) {
      window.__streamAnalyzerHLSChart = echarts.init(dom);
      window.addEventListener('resize', function () {
        if (window.__streamAnalyzerHLSChart) window.__streamAnalyzerHLSChart.resize();
      });
    }
    return window.__streamAnalyzerHLSChart;
  };

  window.disposeHLSChart = function () {
    if (window.__streamAnalyzerHLSChart) {
      window.__streamAnalyzerHLSChart.dispose();
      window.__streamAnalyzerHLSChart = null;
    }
  };

  function tick90kToMs(t) {
    if (t === undefined || t === null || t < 0) return null;
    const n = Number(t);
    if (!isFinite(n)) return null;
    return n / 90;
  }

  /**
   * HLS：按切片绘制（与 FLV 帧级图表独立）。
   * @param {'hls-ts'|'hls-avdts'|'hls-iframe'} type
   * @param {Array} segments - hls_segments
   * @param {string} domId
   */
  window.renderHLSChart = function (type, segments, domId) {
    const chart = window.ensureHLSChart(domId);
    if (!chart) return;
    if (!segments || segments.length === 0) {
      chart.clear();
      chart.setOption({ title: { text: '暂无切片数据', left: 'center', top: 'middle' } });
      return;
    }
    const sorted = segments.slice().sort(function (a, b) {
      return (Number(a.seq) || 0) - (Number(b.seq) || 0);
    });
    const xCat = sorted.map(function (_, i) {
      return String(i);
    });

    function axisTipFormatter(params) {
      if (!params || !params.length) return '';
      const idx = params[0].dataIndex;
      const seg = sorted[idx];
      if (!seg) return '';
      let h = 'playlist#' + seg.seq + '<br/>';
      if (seg.uri) {
        const u = String(seg.uri);
        h += (u.length > 96 ? u.slice(0, 96) + '…' : u) + '<br/>';
      }
      for (let i = 0; i < params.length; i++) {
        const p = params[i];
        const v = p.value;
        if (v === null || v === undefined || (typeof v === 'number' && Number.isNaN(v))) continue;
        h +=
          p.marker +
          p.seriesName +
          ': ' +
          (typeof v === 'number' ? v.toFixed(2) : v) +
          ' ms<br/>';
      }
      return h;
    }

    let option = {};

    switch (type) {
      case 'hls-ts':
        option = {
          title: { text: '切片时间戳（首帧 PTS/DTS，ms）', left: 'center' },
          tooltip: { trigger: 'axis', formatter: axisTipFormatter },
          legend: { data: ['视频PTS', '视频DTS', '音频PTS', '音频DTS'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '切片序号（0起）', data: xCat },
          yAxis: { type: 'value', name: 'ms', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '视频PTS',
              type: 'line',
              data: sorted.map(function (s) {
                return tick90kToMs(s.video_pts_first_90k);
              }),
              showSymbol: sorted.length < 80,
            },
            {
              name: '视频DTS',
              type: 'line',
              data: sorted.map(function (s) {
                return tick90kToMs(s.video_dts_first_90k);
              }),
              showSymbol: sorted.length < 80,
            },
            {
              name: '音频PTS',
              type: 'line',
              data: sorted.map(function (s) {
                return tick90kToMs(s.audio_pts_first_90k);
              }),
              showSymbol: sorted.length < 80,
            },
            {
              name: '音频DTS',
              type: 'line',
              data: sorted.map(function (s) {
                return tick90kToMs(s.audio_dts_first_90k);
              }),
              showSymbol: sorted.length < 80,
            },
          ],
        };
        break;
      case 'hls-avdts':
        option = {
          title: { text: 'AV DTS 差（首帧 V-DTS − A-DTS，ms）', left: 'center' },
          tooltip: {
            trigger: 'axis',
            formatter: function (params) {
              if (!params || !params.length) return '';
              const idx = params[0].dataIndex;
              const seg = sorted[idx];
              if (!seg) return '';
              const v = params[0].value;
              const lv = avSyncLevel(v);
              let h = 'playlist#' + seg.seq + '<br/>偏差: ' + (v != null && !Number.isNaN(v) ? v.toFixed(2) : '-') + ' ms<br/>';
              h += '等级: <span style="color:' + lv.color + ';">' + lv.label + '</span>';
              return h;
            },
          },
          legend: { data: ['AV DTS差'], top: 28 },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '切片序号（0起）', data: xCat },
          yAxis: { type: 'value', name: 'ms', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: 'AV DTS差',
              type: 'line',
              data: sorted.map(function (s) {
                if (!s.av_diff_dts_valid) return null;
                return Number(s.av_diff_dts_90k) / 90;
              }),
              showSymbol: sorted.length < 80,
              markLine: {
                symbol: 'none',
                lineStyle: { type: 'dashed', color: '#9ca3af' },
                data: [{ yAxis: 0, name: '0ms' }],
              },
            },
          ],
        };
        break;
      case 'hls-iframe':
        option = {
          title: { text: '切片内 I 帧间隔（相邻 IDR，ms）', left: 'center' },
          tooltip: {
            trigger: 'axis',
            formatter: function (params) {
              if (!params || !params.length) return '';
              const idx = params[0].dataIndex;
              const seg = sorted[idx];
              if (!seg) return '';
              const arr = seg.iframe_intervals_ms || [];
              let h = 'playlist#' + seg.seq + '<br/>';
              h += arr.length ? '全部间隔: ' + arr.join(', ') + ' ms' : '无足够 IDR（切片内少于 2 个关键帧）';
              return h;
            },
          },
          grid: baseGrid(),
          xAxis: { type: 'category', name: '切片序号（0起）', data: xCat },
          yAxis: { type: 'value', name: 'ms', scale: true },
          dataZoom: [
            { type: 'inside', start: 0, end: 100 },
            { type: 'slider', start: 0, end: 100, height: 18 },
          ],
          series: [
            {
              name: '最大间隔',
              type: 'bar',
              data: sorted.map(function (s) {
                const arr = s.iframe_intervals_ms || [];
                if (!arr.length) return null;
                let m = Number(arr[0]);
                for (let i = 1; i < arr.length; i++) {
                  const n = Number(arr[i]);
                  if (n > m) m = n;
                }
                return m;
              }),
            },
          ],
        };
        break;
      default:
        option = {
          title: { text: '未知 HLS 图表', left: 'center' },
        };
    }

    chart.setOption(option, true);
  };
})();
