package analyzer

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/yutopp/go-amf0"
	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

type PreviewHub struct {
	mu      sync.RWMutex
	subs    map[string][]chan []byte
	bufSize int
}

func NewPreviewHub() *PreviewHub {
	return &PreviewHub{
		subs:    make(map[string][]chan []byte),
		bufSize: 256,
	}
}

func (h *PreviewHub) Subscribe(taskID string) chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, h.bufSize)
	h.subs[taskID] = append(h.subs[taskID], ch)
	return ch
}

func (h *PreviewHub) Unsubscribe(taskID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[taskID]
	for i, c := range subs {
		if c == ch {
			h.subs[taskID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(h.subs[taskID]) == 0 {
		delete(h.subs, taskID)
	}
}

func (h *PreviewHub) Broadcast(taskID string, data []byte) {
	h.mu.RLock()
	subs := h.subs[taskID]
	h.mu.RUnlock()
	if len(subs) == 0 {
		return
	}
	for _, ch := range subs {
		select {
		case ch <- data:
		default:
		}
	}
}

type PreviewEncoder struct {
	header []byte
	buf    bytes.Buffer
	enc    *flv.Encoder
}

func NewPreviewEncoder() (*PreviewEncoder, error) {
	p := &PreviewEncoder{}
	enc, err := flv.NewEncoder(&p.buf, flv.FlagsAudio|flv.FlagsVideo)
	if err != nil {
		return nil, err
	}
	p.enc = enc
	p.header = make([]byte, p.buf.Len())
	copy(p.header, p.buf.Bytes())
	return p, nil
}

func (p *PreviewEncoder) Header() []byte {
	return p.header
}

func (p *PreviewEncoder) Encode(t *tag.FlvTag) ([]byte, error) {
	before := p.buf.Len()
	if err := p.enc.Encode(t); err != nil {
		return nil, err
	}
	after := p.buf.Len()
	if after > before {
		data := make([]byte, after-before)
		copy(data, p.buf.Bytes()[before:after])
		return data, nil
	}
	return nil, nil
}

func cloneFlvTag(t *tag.FlvTag) (*tag.FlvTag, error) {
	cloned := &tag.FlvTag{
		TagType:   t.TagType,
		Timestamp: t.Timestamp,
		StreamID:  t.StreamID,
	}

	switch data := t.Data.(type) {
	case *tag.VideoData:
		if data == nil {
			cloned.Data = nil
			break
		}
		if data.Data != nil {
			raw, err := io.ReadAll(data.Data)
			if err != nil {
				return nil, err
			}
			data.Data = io.NopCloser(bytes.NewReader(raw))
			cpy := *data
			cpy.Data = io.NopCloser(bytes.NewReader(raw))
			cloned.Data = &cpy
		} else {
			cpy := *data
			cloned.Data = &cpy
		}
	case *tag.AudioData:
		if data == nil {
			cloned.Data = nil
			break
		}
		if data.Data != nil {
			raw, err := io.ReadAll(data.Data)
			if err != nil {
				return nil, err
			}
			data.Data = io.NopCloser(bytes.NewReader(raw))
			cpy := *data
			cpy.Data = io.NopCloser(bytes.NewReader(raw))
			cloned.Data = &cpy
		} else {
			cpy := *data
			cloned.Data = &cpy
		}
	case *tag.ScriptData:
		if data == nil {
			cloned.Data = nil
			break
		}
		cpy := *data
		cpy.Objects = make(map[string]amf0.ECMAArray)
		for k, v := range data.Objects {
			cpy.Objects[k] = v
		}
		cloned.Data = &cpy
	default:
		return nil, fmt.Errorf("unknown tag type: %T", t.Data)
	}

	return cloned, nil
}
