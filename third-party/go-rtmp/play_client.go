package rtmp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/yutopp/go-rtmp/message"
)

// PlayTarget describes RTMP play endpoint parsed from URL.
type PlayTarget struct {
	Protocol   string
	Host       string
	App        string
	StreamName string
	TCURL      string
}

type PlayOptions struct {
	ChunkSize uint32
	Start     int64
}

func ParsePlayURL(rawURL string) (*PlayTarget, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse rtmp url")
	}

	protocol := strings.ToLower(u.Scheme)
	if protocol != "rtmp" && protocol != "rtmps" {
		return nil, errors.Errorf("unsupported protocol: %s", u.Scheme)
	}

	if u.Host == "" {
		return nil, errors.New("rtmp url has empty host")
	}

	host := u.Host
	if u.Port() == "" {
		if protocol == "rtmps" {
			host = u.Hostname() + ":443"
		} else {
			host = u.Hostname() + ":1935"
		}
	}

	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.Errorf("invalid stream path %q, expect /<app>/<stream>", u.Path)
	}

	app := parts[0]
	streamName := parts[1]
	if u.RawQuery != "" {
		streamName += "?" + u.RawQuery
	}

	tcURL := fmt.Sprintf("%s://%s/%s", protocol, host, app)

	return &PlayTarget{
		Protocol:   protocol,
		Host:       host,
		App:        app,
		StreamName: streamName,
		TCURL:      tcURL,
	}, nil
}

func DialAndPlay(ctx context.Context, rawURL string, handler Handler, opts *PlayOptions) (*ClientConn, *Stream, *PlayTarget, error) {
	target, err := ParsePlayURL(rawURL)
	if err != nil {
		return nil, nil, nil, err
	}

	connConfig := &ConnConfig{
		Handler: handler,
	}

	var cc *ClientConn
	if target.Protocol == "rtmps" {
		cc, err = TLSDial(target.Protocol, target.Host, connConfig, nil)
	} else {
		cc, err = Dial(target.Protocol, target.Host, connConfig)
	}
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to dial")
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = cc.Close()
		}
	}()

	connect := &message.NetConnectionConnect{
		Command: message.NetConnectionConnectCommand{
			App:           target.App,
			Type:          "nonprivate",
			FlashVer:      "FMLE/3.0 (compatible; streamanalyzer)",
			TCURL:         target.TCURL,
			Fpad:          false,
			Capabilities:  15,
			AudioCodecs:   3191,
			VideoCodecs:   252,
			VideoFunction: 1,
		},
	}
	if err := cc.Connect(connect); err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to connect")
	}

	chunkSize := uint32(0)
	start := int64(-2)
	if opts != nil {
		chunkSize = opts.ChunkSize
		start = opts.Start
	}

	stream, err := cc.CreateStream(nil, chunkSize)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to create stream")
	}

	if err := stream.Play(&message.NetStreamPlay{
		StreamName: target.StreamName,
		Start:      start,
	}); err != nil {
		_ = stream.Close()
		return nil, nil, nil, errors.Wrap(err, "failed to send play command")
	}

	if ctx != nil {
		go func() {
			<-ctx.Done()
			_ = stream.Close()
			_ = cc.Close()
		}()
	}

	cleanup = false
	return cc, stream, target, nil
}
