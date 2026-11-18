package diagnostic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	tsHTTPReadSize = 64 << 10
	tsMaxDatagram  = 65_507
)

// A span covers one HTTP body read or one whole UDP/RTP datagram. RTP headers
// and padding remain in the dump, and the offsets always address the dump.
type tsSpan struct {
	Offset    int64 `json:"offset"`
	Size      int   `json:"size"`
	ArrivalMS int64 `json:"arrival_ms"`
}

type tsCapture struct {
	stream      *artifactWriter
	timeline    *artifactWriter
	observation *Observation
	start       time.Time
}

func sampleTS(ctx context.Context, client *http.Client, dir string, target *url.URL, seconds int, protocol string) (Report, error) {
	stream, err := newArtifact(dir, dumpName(protocol))
	if err != nil {
		return Report{}, err
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		return Report{}, errors.Join(err, stream.close())
	}

	start := time.Now()
	obs := Observation{Origin: origin(target), StartedAt: start.UTC(), RequestedSeconds: seconds}
	capture := tsCapture{stream: stream, timeline: timeline, observation: &obs, start: start}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var captureErr error
	if protocol == ProtocolHTTPTS {
		captureErr = capture.http(ctx, client, target)
	} else {
		captureErr = capture.datagrams(ctx, target)
	}
	obs.ElapsedMS = time.Since(start).Milliseconds()
	if err := errors.Join(captureErr, stream.close(), timeline.close()); err != nil {
		return Report{}, err
	}
	manifest := Manifest{
		Version: multiProtocolSchemaVersion, Protocol: protocol, Observation: obs,
		Stream: stream.artifact(), Timeline: timeline.artifact(),
	}
	return replayTS(dir, manifest)
}

// keep returns false when a limit is reached. For datagrams this retains a
// prefix of whole datagrams, so a truncated datagram never looks like one
// the sender actually sent.
func (c *tsCapture) keep(data []byte) (bool, error) {
	if int64(len(data)) > maxStreamBytes-c.stream.bytes {
		c.observation.EndReason = "size_limit"
		return false, nil
	}
	span := tsSpan{Offset: c.stream.bytes, Size: len(data), ArrivalMS: time.Since(c.start).Milliseconds()}
	record, err := json.Marshal(span)
	if err != nil {
		return false, err
	}
	record = append(record, '\n')
	if int64(len(record)) > maxTimeline-c.timeline.bytes {
		c.observation.EndReason = "timeline_limit"
		return false, nil
	}
	if len(data) > 0 {
		markFirstPayload(c.observation, c.start)
	}
	if _, err := c.stream.Write(data); err != nil {
		return false, err
	}
	if _, err := c.timeline.Write(record); err != nil {
		return false, err
	}
	if c.stream.bytes == maxStreamBytes {
		c.observation.EndReason = "size_limit"
		return false, nil
	}
	return true, nil
}

func (c *tsCapture) http(ctx context.Context, client *http.Client, target *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		c.observation.FailureStage, c.observation.FailureCode = httpFailure("request_send", err)
		c.observation.EndReason = "request_error"
		return nil
	}
	req.Header.Set("User-Agent", "StreamAnalyzer-Diagnostic/1.0")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := doObservedRequest(client, req, c.observation, c.start)
	if err != nil {
		c.observation.EndReason = captureEnd(ctx, "request_error")
		return nil
	}
	defer resp.Body.Close()
	c.observation.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		c.observation.EndReason = "http_status"
		c.observation.FailureStage, c.observation.FailureCode = "response_headers", "http_status"
		return nil
	}
	finishReceive := c.beginReceive()
	defer finishReceive()

	buffer := make([]byte, tsHTTPReadSize)
	for ctx.Err() == nil {
		readLimit := int(min(int64(len(buffer)), maxStreamBytes-c.stream.bytes))
		n, readErr := resp.Body.Read(buffer[:readLimit])
		if n > 0 {
			more, err := c.keep(buffer[:n])
			if err != nil || !more {
				return err
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			c.observation.EndReason = captureEnd(ctx, "eof")
		} else {
			c.markReadFailure(ctx, "body_error")
		}
		return nil
	}
	c.observation.EndReason = captureEnd(ctx, "")
	return nil
}

func (c *tsCapture) datagrams(ctx context.Context, target *url.URL) error {
	if ctx.Err() != nil {
		c.observation.EndReason = captureEnd(ctx, "")
		return nil
	}
	conn, err := listenTSDatagrams(target)
	c.observation.Stages = append(c.observation.Stages, CaptureStage{
		Name: "listen", EndMS: time.Since(c.start).Milliseconds(),
	})
	if err != nil {
		c.observation.EndReason = captureEnd(ctx, "udp_bind_error")
		if c.observation.EndReason == "udp_bind_error" {
			c.observation.FailureStage, c.observation.FailureCode = "listen", "udp_bind_error"
		}
		return nil
	}
	defer conn.Close()
	// The OS may clamp the socket buffer to its configured maximum.
	_ = conn.SetReadBuffer(4 << 20)
	return c.receiveDatagrams(ctx, conn)
}

func listenTSDatagrams(target *url.URL) (*net.UDPConn, error) {
	address, err := netip.ParseAddrPort(target.Host)
	if err != nil || !address.Addr().Is4() {
		return nil, fmt.Errorf("invalid IPv4 UDP listen address")
	}
	udpAddress := net.UDPAddrFromAddrPort(address)
	if !address.Addr().IsMulticast() {
		return net.ListenUDP("udp4", udpAddress)
	}
	var iface *net.Interface
	if name := target.Query().Get("interface"); name != "" {
		iface, err = net.InterfaceByName(name)
		if err != nil {
			return nil, err
		}
	}
	return net.ListenMulticastUDP("udp4", iface, udpAddress)
}

func (c *tsCapture) receiveDatagrams(ctx context.Context, conn *net.UDPConn) error {
	finishReceive := c.beginReceive()
	defer finishReceive()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	buffer := make([]byte, tsMaxDatagram)
	for ctx.Err() == nil {
		n, readErr := conn.Read(buffer)
		if readErr != nil {
			c.markReadFailure(ctx, "udp_read_error")
			return nil
		}
		more, err := c.keep(buffer[:n])
		if err != nil || !more {
			return err
		}
	}
	c.observation.EndReason = captureEnd(ctx, "")
	return nil
}

func (c *tsCapture) beginReceive() func() {
	index := len(c.observation.Stages)
	c.observation.Stages = append(c.observation.Stages, CaptureStage{
		Name: "receive", StartMS: time.Since(c.start).Milliseconds(),
	})
	return func() {
		c.observation.Stages[index].EndMS = time.Since(c.start).Milliseconds()
		if c.observation.EndReason == "duration_limit" && c.observation.FirstPayloadMS == nil {
			c.observation.FailureStage, c.observation.FailureCode = "receive", "timeout"
		}
		recordHTTPBody(c.observation, c.observation.Stages[index])
	}
}

func (c *tsCapture) markReadFailure(ctx context.Context, reason string) {
	c.observation.EndReason = captureEnd(ctx, reason)
	if c.observation.EndReason == reason {
		c.observation.FailureStage, c.observation.FailureCode = "receive", reason
	}
}

// walkTSChunks validates stored spans as it reads the dump. The supplied data
// aliases a reusable buffer, so a consumer must copy bytes it retains.
func walkTSChunks(dir string, manifest Manifest, visit func(tsSpan, []byte) error) error {
	limit := tsHTTPReadSize
	switch manifest.Protocol {
	case ProtocolUDPTS, ProtocolRTPTS:
		limit = tsMaxDatagram
	case ProtocolHTTPTS:
	default:
		return fmt.Errorf("%w: 不支持的 MPEG-TS 传输协议", ErrInvalid)
	}
	stream, err := os.Open(filepath.Join(dir, dumpName(manifest.Protocol)))
	if err != nil {
		return err
	}
	defer stream.Close()
	timeline, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return err
	}
	defer timeline.Close()
	buffer := make([]byte, limit)
	reader := bufio.NewReaderSize(stream, tsHTTPReadSize)
	scanner := bufio.NewScanner(io.LimitReader(timeline, maxTimeline+1))
	scanner.Buffer(make([]byte, 128), 512)
	var offset, lastArrival int64
	for scanner.Scan() {
		span := tsSpan{Offset: -1, Size: -1, ArrivalMS: -1}
		if json.Unmarshal(scanner.Bytes(), &span) != nil ||
			span.Offset != offset || span.Size < 0 || span.Size > limit ||
			(manifest.Protocol == ProtocolHTTPTS && span.Size == 0) ||
			int64(span.Size) > manifest.Stream.Bytes-offset ||
			span.ArrivalMS < lastArrival || span.ArrivalMS > manifest.Observation.ElapsedMS {
			return fmt.Errorf("%w: MPEG-TS 时间线与原始流不一致", ErrInvalid)
		}
		data := buffer[:span.Size]
		if _, err := io.ReadFull(reader, data); err != nil {
			return fmt.Errorf("%w: MPEG-TS 原始流数据不完整", ErrInvalid)
		}
		if err := visit(span, data); err != nil {
			return err
		}
		offset += int64(span.Size)
		lastArrival = span.ArrivalMS
	}
	if scanner.Err() != nil || offset != manifest.Stream.Bytes {
		return fmt.Errorf("%w: MPEG-TS 时间线不完整", ErrInvalid)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: MPEG-TS 原始流包含未记录的数据", ErrInvalid)
	}
	return nil
}
