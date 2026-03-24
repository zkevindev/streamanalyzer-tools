package analyzer

import (
	"context"
	"fmt"
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

func newHTTPFLVDecoder(resp *http.Response) (*httpFLVDecoder, error) {
	decoder, err := flv.NewDecoder(resp.Body)
	if err != nil {
		return nil, err
	}
	return &httpFLVDecoder{
		decoder: decoder,
		resp:    resp,
	}, nil
}

func (s *StreamAnalyzer) runHTTPFLVTask(ctx context.Context, at *analyzeTask) {
	client := &http.Client{Timeout: 0}

	req, err := http.NewRequestWithContext(ctx, "GET", at.URL, nil)
	if err != nil {
		fmt.Printf("HTTP FLV request error: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", "FLVAnalyzer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("HTTP FLV do error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP FLV status: %d\n", resp.StatusCode)
		return
	}

	decoder, err := newHTTPFLVDecoder(resp)
	if err != nil {
		fmt.Printf("FLV decoder error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	s.readFLVStream(ctx, decoder, at)
}

func (s *StreamAnalyzer) readFLVStream(ctx context.Context, decoder FlvDecoder, at *analyzeTask) {
	for {
		flvTag := &tag.FlvTag{}
		if err := decoder.Decode(flvTag); err != nil {
			if shouldStopDecode(err) || ctx.Err() != nil {
				return
			}
			fmt.Printf("FLV decode error: %v\n", err)
			return
		}

		s.handleFLVTag(flvTag, at)
	}
}

// RTMP path: go-rtmp first parses RTMP chunk messages, then OnAudio/OnVideo
// decodes message payload into FLV audio/video tags.
func (s *StreamAnalyzer) readRTMPStream(ctx context.Context, decoder *RTMPDecoder, at *analyzeTask) {
	for {
		flvTag := &tag.FlvTag{}
		if err := decoder.Decode(flvTag); err != nil {
			if shouldStopDecode(err) || ctx.Err() != nil {
				return
			}
			fmt.Printf("RTMP decode error: %v\n", err)
			return
		}

		s.handleFLVTag(flvTag, at)
	}
}
