package analyzer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/yutopp/go-amf0"
	"github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

type rtmpClientHandler struct {
	rtmp.DefaultHandler
	streamCh  chan *tag.FlvTag
	closeOnce sync.Once
}

func newRTMPClientHandler() *rtmpClientHandler {
	return &rtmpClientHandler{
		streamCh: make(chan *tag.FlvTag, 100),
	}
}

func (h *rtmpClientHandler) emitTag(flvTag *tag.FlvTag) {
	defer func() {
		_ = recover() // ignore send on closed channel
	}()
	h.streamCh <- flvTag
}

func (h *rtmpClientHandler) OnConnect(timestamp uint32, cmd *rtmpmsg.NetConnectionConnect) error {
	fmt.Printf("RTMP connected\n")
	return nil
}

func (h *rtmpClientHandler) OnCreateStream(timestamp uint32, cmd *rtmpmsg.NetConnectionCreateStream) error {
	fmt.Printf("RTMP stream created\n")
	return nil
}

func (h *rtmpClientHandler) OnPlay(ctx *rtmp.StreamContext, timestamp uint32, cmd *rtmpmsg.NetStreamPlay) error {
	fmt.Printf("RTMP play: %s\n", cmd.StreamName)
	return nil
}

func (h *rtmpClientHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	var audio tag.AudioData
	if err := tag.DecodeAudioData(payload, &audio); err != nil {
		return err
	}
	if audio.Data != nil {
		raw, err := io.ReadAll(audio.Data)
		if err != nil {
			return err
		}
		audio.Data = bytes.NewReader(raw)
	}

	flvTag := &tag.FlvTag{
		TagType:   tag.TagTypeAudio,
		Timestamp: timestamp,
		Data:      &audio,
	}
	h.emitTag(flvTag)

	return nil
}

func (h *rtmpClientHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	var video tag.VideoData
	if err := tag.DecodeVideoData(payload, &video); err != nil {
		return err
	}
	if video.Data != nil {
		raw, err := io.ReadAll(video.Data)
		if err != nil {
			return err
		}
		video.Data = bytes.NewReader(raw)
	}

	flvTag := &tag.FlvTag{
		TagType:   tag.TagTypeVideo,
		Timestamp: timestamp,
		Data:      &video,
	}
	h.emitTag(flvTag)

	return nil
}

func (h *rtmpClientHandler) OnSetDataFrame(timestamp uint32, data *rtmpmsg.NetStreamSetDataFrame) error {
	script := &tag.ScriptData{
		Objects: map[string]amf0.ECMAArray{},
	}

	switch v := data.AmfData.(type) {
	case []interface{}:
		if len(v) > 0 {
			switch x := v[0].(type) {
			case amf0.ECMAArray:
				script.Objects["onMetaData"] = x
			case map[string]interface{}:
				script.Objects["onMetaData"] = amf0.ECMAArray(x)
			}
		}
	case amf0.ECMAArray:
		script.Objects["onMetaData"] = v
	case map[string]interface{}:
		script.Objects["onMetaData"] = amf0.ECMAArray(v)
	}

	h.emitTag(&tag.FlvTag{
		TagType:   tag.TagTypeScriptData,
		Timestamp: timestamp,
		Data:      script,
	})

	return nil
}

func (h *rtmpClientHandler) OnClose() {
	fmt.Printf("RTMP connection closed\n")
	h.closeOnce.Do(func() {
		close(h.streamCh)
	})
}

type RTMPClientConn struct {
	*rtmp.ClientConn
	stream  *rtmp.Stream
	target  *rtmp.PlayTarget
	handler *rtmpClientHandler
}

func DialRTMP(ctx context.Context, rtmpURL string) (*RTMPClientConn, error) {
	handler := newRTMPClientHandler()
	client, stream, target, err := rtmp.DialAndPlay(ctx, rtmpURL, handler, &rtmp.PlayOptions{
		ChunkSize: 128,
		Start:     -2,
	})
	if err != nil {
		return nil, err
	}

	fmt.Printf("Connected to %s, app=%s, stream=%s\n", target.Host, target.App, target.StreamName)

	return &RTMPClientConn{
		ClientConn: client,
		stream:     stream,
		target:     target,
		handler:    handler,
	}, nil
}

func (cc *RTMPClientConn) StreamCh() <-chan *tag.FlvTag {
	return cc.handler.streamCh
}

type RTMPDecoder struct {
	conn     *RTMPClientConn
	streamCh <-chan *tag.FlvTag
	doneCh   <-chan struct{}
}

func NewRTMPDecoder(ctx context.Context, rtmpURL string) (*RTMPDecoder, error) {
	conn, err := DialRTMP(ctx, rtmpURL)
	if err != nil {
		return nil, err
	}

	return &RTMPDecoder{
		conn:     conn,
		streamCh: conn.StreamCh(),
		doneCh:   ctx.Done(),
	}, nil
}

func (d *RTMPDecoder) Decode(flvTag *tag.FlvTag) error {
	if d.doneCh == nil {
		tag, ok := <-d.streamCh
		if !ok || tag == nil {
			return io.EOF
		}
		*flvTag = *tag
		return nil
	}

	select {
	case tag, ok := <-d.streamCh:
		if !ok || tag == nil {
			return io.EOF
		}
		*flvTag = *tag
		return nil
	case <-d.doneCh:
		return context.Canceled
	}
}

func (d *RTMPDecoder) Close() error {
	if d.conn.stream != nil {
		_ = d.conn.stream.Close()
	}
	return d.conn.Close()
}
