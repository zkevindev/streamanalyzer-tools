package analyzer

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

type FlvDecoder interface {
	Decode(flvTag *tag.FlvTag) error
}

type httpFLVDecoder struct {
	decoder *flv.Decoder
	resp    *http.Response
}

func (d *httpFLVDecoder) Decode(flvTag *tag.FlvTag) error {
	return d.decoder.Decode(flvTag)
}

// newHTTPFLVDecoder reads from resp.Body. If dumpW is non-nil, the untouched
// response body (a well-formed FLV file) is copied to it as it is consumed.
func newHTTPFLVDecoder(resp *http.Response, dumpW io.Writer) (*httpFLVDecoder, error) {
	var body io.Reader = resp.Body
	if dumpW != nil {
		body = io.TeeReader(body, dumpW)
	}
	decoder, err := flv.NewDecoder(body)
	if err != nil {
		return nil, err
	}
	return &httpFLVDecoder{
		decoder: decoder,
		resp:    resp,
	}, nil
}

func (s *StreamAnalyzer) runHTTPFLVTask(ctx context.Context, at *analyzeTask) error {
	client := &http.Client{Timeout: 0}

	req, err := http.NewRequestWithContext(ctx, "GET", at.URL, nil)
	if err != nil {
		return &taskStartupError{err: err}
	}
	req.Header.Set("User-Agent", "FLVAnalyzer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return &taskStartupError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &taskStartupError{err: fmt.Errorf("http flv status: %d", resp.StatusCode)}
	}

	var dumpW io.Writer
	if at.dump != nil {
		dumpW = at.dump.Writer(at.ID + ".flv")
	}

	decoder, err := newHTTPFLVDecoder(resp, dumpW)
	if err != nil {
		return &taskStartupError{err: err}
	}

	return s.readFLVStream(ctx, decoder, at)
}

func (s *StreamAnalyzer) readFLVStream(ctx context.Context, decoder FlvDecoder, at *analyzeTask) error {
	for {
		flvTag := &tag.FlvTag{}
		if err := decoder.Decode(flvTag); err != nil {
			if shouldStopDecode(err) || ctx.Err() != nil {
				return err
			}
			return err
		}

		if at.previewHub != nil && at.previewEnc != nil {
			if cloned, err := cloneFlvTag(flvTag); err == nil {
				if data, err := at.previewEnc.Encode(cloned); err == nil && len(data) > 0 {
					at.previewHub.Broadcast(at.ID, data)
				}
			}
		}

		s.handleFLVTag(flvTag, at)
	}
}

// RTMP path: go-rtmp first parses RTMP chunk messages, then OnAudio/OnVideo
// decodes message payload into FLV audio/video tags.
func (s *StreamAnalyzer) readRTMPStream(ctx context.Context, decoder *RTMPDecoder, at *analyzeTask) error {
	for {
		flvTag := &tag.FlvTag{}
		if err := decoder.Decode(flvTag); err != nil {
			if shouldStopDecode(err) || ctx.Err() != nil {
				return err
			}
			return err
		}

		if at.previewHub != nil && at.previewEnc != nil {
			if cloned, err := cloneFlvTag(flvTag); err == nil {
				if data, err := at.previewEnc.Encode(cloned); err == nil && len(data) > 0 {
					at.previewHub.Broadcast(at.ID, data)
				}
			}
		}

		s.handleFLVTag(flvTag, at)
	}
}
