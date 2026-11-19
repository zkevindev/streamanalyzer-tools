package diagnostic

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/g711"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/pion/rtp"
)

type rtspG711Input struct {
	payload []byte
	pts     int64
	ssrc    uint32
}
type rtspG711Config struct {
	codec          string
	rate, channels int
	tool           string
}

// rtspG711Encoder owns one conversion process. Native RTP callbacks only copy
// bounded payloads into queue; no process IO or sample conversion runs there.
type rtspG711Encoder struct {
	mu               sync.Mutex
	config           rtspG711Config
	report           RTSPG711PreviewReport
	queue            chan rtspG711Input
	done             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	command          *exec.Cmd
	stdin            io.WriteCloser
	stdout, metadata io.ReadCloser
	closed, failed   bool
	anchor           int64
	hasAnchor        bool
	firstInput       time.Time
	emit             func(int64, []byte, []byte) bool
	onFailure        func(string)
}

func rtspG711Args(config rtspG711Config) []string {
	bitrate := "48k"
	if config.channels == 2 {
		bitrate = "80k"
	}
	return []string{"-hide_banner", "-loglevel", "quiet", "-nostats", "-probesize", "32", "-analyzeduration", "1", "-protocol_whitelist", "pipe", "-f", "s16be", "-ar", strconv.Itoa(config.rate), "-ac", strconv.Itoa(config.channels), "-i", "pipe:0", "-map", "0:a:0", "-c:a", "aac", "-profile:a", "aac_low", "-ar", "48000", "-ac", strconv.Itoa(config.channels), "-b:a", bitrate, "-threads", "1", "-flush_packets", "1", "-f", "tee", "[f=adts]pipe:1|[f=framecrc]pipe:2"}
}

func rtspG711Configuration(codec string, rate, channels int) bool {
	if codec != "pcma" && codec != "pcmu" || channels < 1 || channels > 2 {
		return false
	}
	switch rate {
	case 8000, 16000, 32000, 48000:
		return true
	}
	return false
}

// qualifyRTSPG711 checks a small constant probe before a FLV header advertises
// audio. Probe bytes are discarded and never counted or published as media.
func qualifyRTSPG711(config rtspG711Config) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, config.tool, rtspG711Args(config)...)
	command.WaitDelay = 250 * time.Millisecond
	command.Stdin = bytes.NewReader(make([]byte, 320*config.channels))
	var output, metadata rtspG711ProbeBuffer
	command.Stdout = &output
	command.Stderr = &metadata
	if command.Run() != nil || output.over || metadata.over {
		return false
	}
	adts := bytes.NewReader(output.Bytes())
	crc := newRTSPG711CRCReader(bytes.NewReader(metadata.Bytes()), config.channels)
	count := 0
	for {
		au, err := readRTSPG711ADTS(adts, config.channels)
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		frame, err := crc.next()
		if err != nil || frame.size != len(au) || frame.checksum != rtspG711Adler(au) {
			return false
		}
		count++
	}
	_, err := crc.next()
	return count > 0 && err == io.EOF
}

type rtspG711ProbeBuffer struct {
	bytes.Buffer
	over bool
}

func (buffer *rtspG711ProbeBuffer) Write(data []byte) (int, error) {
	if buffer.Len()+len(data) > 16<<10 {
		buffer.over = true
		return 0, io.ErrShortBuffer
	}
	return buffer.Buffer.Write(data)
}

func newRTSPG711Encoder(config rtspG711Config, lookup func() (string, error), emit func(int64, []byte, []byte) bool, onFailure func(string)) *rtspG711Encoder {
	encoder := &rtspG711Encoder{config: config, queue: make(chan rtspG711Input, rtspG711QueueSize), done: make(chan struct{}), emit: emit, onFailure: onFailure, report: RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "starting", InputSampleRate: config.rate, OutputSampleRate: rtspG711OutputRate, Channels: config.channels}}
	if !rtspG711Configuration(config.codec, config.rate, config.channels) {
		encoder.unavailable("g711_configuration_unsupported")
		return encoder
	}
	start := time.Now()
	if config.tool == "" {
		if lookup == nil {
			lookup = func() (string, error) { return investigationMediaTool("ffmpeg", "") }
		}
		var err error
		config.tool, err = lookup()
		if err != nil {
			elapsed := time.Since(start).Milliseconds()
			encoder.report.ToolCheckMS = &elapsed
			encoder.unavailable("ffmpeg_unavailable")
			return encoder
		}
		encoder.config.tool = config.tool
	}
	if !qualifyRTSPG711(config) {
		elapsed := time.Since(start).Milliseconds()
		encoder.report.ToolCheckMS = &elapsed
		encoder.unavailable("ffmpeg_unsupported")
		return encoder
	}
	elapsed := time.Since(start).Milliseconds()
	encoder.report.ToolCheckMS = &elapsed
	encoder.ctx, encoder.cancel = context.WithTimeout(context.Background(), time.Duration(maxSeconds+5)*time.Second)
	encoder.command = exec.CommandContext(encoder.ctx, config.tool, rtspG711Args(config)...)
	encoder.command.WaitDelay = 250 * time.Millisecond
	var err error
	encoder.stdin, err = encoder.command.StdinPipe()
	if err == nil {
		encoder.stdout, err = encoder.command.StdoutPipe()
	}
	if err == nil {
		encoder.metadata, err = encoder.command.StderrPipe()
	}
	if err == nil {
		err = encoder.command.Start()
	}
	if err != nil {
		encoder.cancel()
		encoder.closePipes()
		encoder.unavailable("ffmpeg_start_failed")
		return encoder
	}
	go encoder.run()
	return encoder
}

func (encoder *rtspG711Encoder) unavailable(code string) {
	encoder.failed = true
	encoder.report.State = "unavailable"
	encoder.report.ReasonCode = code
	close(encoder.done)
}
func (encoder *rtspG711Encoder) available() bool {
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	return !encoder.failed
}
func (encoder *rtspG711Encoder) closePipes() {
	for _, pipe := range []io.Closer{encoder.stdin, encoder.stdout, encoder.metadata} {
		if pipe != nil {
			pipe.Close()
		}
	}
}

func (encoder *rtspG711Encoder) fail(code string) {
	encoder.mu.Lock()
	if encoder.failed {
		encoder.mu.Unlock()
		return
	}
	encoder.failed = true
	encoder.report.State = "failed"
	encoder.report.ReasonCode = code
	cancel := encoder.cancel
	callback := encoder.onFailure
	encoder.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if callback != nil {
		callback(code)
	}
}

func (encoder *rtspG711Encoder) enqueue(packet *rtp.Packet, pts int64) {
	encoder.mu.Lock()
	if encoder.failed || encoder.closed {
		encoder.mu.Unlock()
		return
	}
	if len(packet.Payload) == 0 || len(packet.Payload)%encoder.config.channels != 0 || len(packet.Payload) > min(64<<10, encoder.config.rate*encoder.config.channels) {
		encoder.report.PacketErrors++
		encoder.mu.Unlock()
		encoder.fail("g711_packet_invalid")
		return
	}
	item := rtspG711Input{payload: bytes.Clone(packet.Payload), pts: pts, ssrc: packet.SSRC}
	select {
	case encoder.queue <- item:
		encoder.mu.Unlock()
	default:
		encoder.report.DroppedPackets++
		encoder.mu.Unlock()
		encoder.fail("g711_queue_limit")
	}
}

func (encoder *rtspG711Encoder) run() {
	defer close(encoder.done)
	defer encoder.cancel()
	defer encoder.closePipes()
	meta := make(chan rtspG711CRC, 32)
	metaDone := make(chan error, 1)
	go func() {
		defer close(meta)
		parser := newRTSPG711CRCReader(encoder.metadata, encoder.config.channels)
		for {
			frame, err := parser.next()
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				metaDone <- err
				return
			}
			select {
			case meta <- frame:
			case <-encoder.ctx.Done():
				metaDone <- encoder.ctx.Err()
				return
			}
		}
	}()
	outputDone := make(chan string, 1)
	go func() {
		code := encoder.readOutput(meta)
		if code != "" {
			encoder.fail(code)
		}
		outputDone <- code
	}()
	encoder.writeInput()
	encoder.stdin.Close()
	outputCode := <-outputDone
	metaError := <-metaDone
	err := encoder.command.Wait()
	encoder.mu.Lock()
	failed := encoder.failed
	encoder.mu.Unlock()
	if !failed && (outputCode != "" || metaError != nil || err != nil) {
		encoder.fail("ffmpeg_failed")
	}
	encoder.mu.Lock()
	if !encoder.failed && encoder.report.OutputAUs == 0 {
		encoder.report.State = "no_media"
		encoder.report.ReasonCode = "audio_no_media"
	}
	encoder.mu.Unlock()
}

func (encoder *rtspG711Encoder) writeInput() {
	var expected int64
	var ssrc uint32
	initialized := false
	for {
		var item rtspG711Input
		select {
		case <-encoder.ctx.Done():
			return
		case value, ok := <-encoder.queue:
			if !ok {
				return
			}
			item = value
		}
		samples := int64(len(item.payload) / encoder.config.channels)
		if !initialized {
			encoder.mu.Lock()
			encoder.anchor = item.pts
			encoder.hasAnchor = true
			encoder.firstInput = time.Now()
			first := item.pts
			encoder.report.FirstInputPTS = &first
			encoder.mu.Unlock()
			expected, ssrc, initialized = item.pts, item.ssrc, true
		}
		delta := item.pts - expected
		if ssrc != item.ssrc || delta > int64(encoder.config.rate)/2 || delta < -int64(encoder.config.rate)/2 {
			encoder.mu.Lock()
			encoder.report.TimelineResets++
			encoder.mu.Unlock()
			encoder.fail("g711_timeline_discontinuity")
			return
		}
		if delta > 0 {
			encoder.mu.Lock()
			silence := encoder.report.InsertedSilenceSamples + delta
			if silence > int64(encoder.config.rate)*5 {
				encoder.report.TimelineResets++
				encoder.mu.Unlock()
				encoder.fail("g711_silence_limit")
				return
			}
			encoder.report.InsertedSilenceSamples = silence
			encoder.mu.Unlock()
			if !encoder.writePCM(make([]byte, int(delta)*encoder.config.channels*2)) {
				return
			}
			expected += delta
		}
		if delta < 0 {
			drop := min(-delta, samples)
			encoder.mu.Lock()
			encoder.report.DroppedLateSamples += drop
			encoder.mu.Unlock()
			item.payload = item.payload[int(drop)*encoder.config.channels:]
			samples -= drop
			if samples == 0 {
				continue
			}
		}
		encoder.mu.Lock()
		encoder.report.InputPackets++
		encoder.report.InputSamples += samples
		supplied := encoder.report.InputSamples + encoder.report.InsertedSilenceSamples
		encoder.mu.Unlock()
		if supplied > int64(maxSeconds+2)*int64(encoder.config.rate) {
			encoder.fail("g711_sample_limit")
			return
		}
		var pcm []byte
		if encoder.config.codec == "pcma" {
			var value g711.Alaw
			value.Unmarshal(item.payload)
			pcm = value
		} else {
			var value g711.Mulaw
			value.Unmarshal(item.payload)
			pcm = value
		}
		if !encoder.writePCM(pcm) {
			return
		}
		expected += samples
	}
}

func (encoder *rtspG711Encoder) writePCM(data []byte) bool {
	for len(data) > 0 {
		n, err := encoder.stdin.Write(data)
		if err != nil || n <= 0 {
			encoder.fail("ffmpeg_failed")
			return false
		}
		data = data[n:]
	}
	return true
}

func (encoder *rtspG711Encoder) readOutput(meta <-chan rtspG711CRC) string {
	reader := bufio.NewReaderSize(encoder.stdout, 8192)
	var previous rtspG711CRC
	hasPrevious := false
	config := &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: rtspG711OutputRate, ChannelConfig: uint8(encoder.config.channels)}
	asc, err := config.Marshal()
	if err != nil {
		return "ffmpeg_output_invalid"
	}
	for {
		au, err := readRTSPG711ADTS(reader, encoder.config.channels)
		if err == io.EOF {
			if _, extra := <-meta; extra {
				return "ffmpeg_output_invalid"
			}
			return ""
		}
		if err != nil {
			return "ffmpeg_output_invalid"
		}
		frame, ok := <-meta
		if !ok || frame.size != len(au) || frame.checksum != rtspG711Adler(au) || hasPrevious && frame.pts != previous.pts+previous.duration {
			return "ffmpeg_output_invalid"
		}
		encoder.mu.Lock()
		anchor, ready := encoder.anchor, encoder.hasAnchor
		firstInput := encoder.firstInput
		supplied := encoder.report.InputSamples + encoder.report.InsertedSilenceSamples
		encoder.mu.Unlock()
		if !ready || frame.pts > supplied*rtspG711OutputRate/int64(encoder.config.rate)+rtspG711AACSamples {
			return "ffmpeg_output_invalid"
		}
		rate := int64(encoder.config.rate)
		// Add the common 2 s preview shift before division, retaining the actual
		// negative encoder priming PTS instead of silently re-zeroing audio.
		numerator := (anchor*rtspG711OutputRate+frame.pts*rate)*1000 + 2000*rate*rtspG711OutputRate
		if numerator < 0 {
			return "g711_timeline_discontinuity"
		}
		timestamp := numerator / (rate * rtspG711OutputRate)
		if timestamp > maxPlayerElapsedMS+2000 {
			return "g711_timeline_discontinuity"
		}
		if !encoder.emit(timestamp, asc, au) {
			return "player_unavailable"
		}
		encoder.mu.Lock()
		if encoder.report.EncoderPrimingSamples == nil {
			priming := max(int64(0), -frame.pts)
			encoder.report.EncoderPrimingSamples = &priming
			startup := time.Since(firstInput).Milliseconds()
			encoder.report.StartupMS = &startup
		}
		padding := int64(rtspG711AACSamples) - frame.duration
		encoder.report.EncoderPaddingSamples = &padding
		encoder.report.OutputAUs++
		encoder.report.OutputSamples += frame.duration
		if !encoder.failed {
			encoder.report.State = "ready"
			encoder.report.ReasonCode = ""
		}
		encoder.mu.Unlock()
		previous, hasPrevious = frame, true
	}
}

func (encoder *rtspG711Encoder) finish() RTSPG711PreviewReport {
	start := time.Now()
	encoder.mu.Lock()
	if !encoder.closed {
		encoder.closed = true
		close(encoder.queue)
	}
	cancel := encoder.cancel
	encoder.mu.Unlock()
	if cancel != nil {
		timer := time.AfterFunc(1500*time.Millisecond, func() { encoder.fail("ffmpeg_drain_timeout") })
		<-encoder.done
		timer.Stop()
	} else {
		<-encoder.done
	}
	encoder.mu.Lock()
	elapsed := time.Since(start).Milliseconds()
	encoder.report.DrainMS = &elapsed
	report := encoder.report
	encoder.mu.Unlock()
	return cloneRTSPG711Report(report)
}

func cloneRTSPG711Report(value RTSPG711PreviewReport) RTSPG711PreviewReport {
	for _, field := range []**int64{&value.FirstInputPTS, &value.EncoderPrimingSamples, &value.EncoderPaddingSamples, &value.ToolCheckMS, &value.StartupMS, &value.DrainMS} {
		if *field != nil {
			copy := **field
			*field = &copy
		}
	}
	return value
}
