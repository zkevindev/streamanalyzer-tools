package rtmp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

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
	TLSConfig *tls.Config

	// RecvTap, if set, receives a copy of every byte read from the connection,
	// starting with the server handshake (S0+S1+S2).
	RecvTap io.Writer

	// Logger, if set, receives the library's internal diagnostics. Without it
	// go-rtmp discards them, which makes protocol-level issues invisible.
	Logger logrus.FieldLogger

	// OnStage receives completed TLS and RTMP setup phases. DNS and TCP
	// connection callbacks can be supplied through the context's httptrace.
	OnStage func(PlayStage)
}

type PlayStage struct {
	Name      string
	StartedAt time.Time
	EndedAt   time.Time
	Err       error
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
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "1935")
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
	if ctx == nil {
		ctx = context.Background()
	}
	target, err := ParsePlayURL(rawURL)
	if err != nil {
		return nil, nil, nil, err
	}

	connConfig := &ConnConfig{
		Handler: handler,
		// Many production servers and CDNs do not echo C1's random bytes back in
		// S2. Rejecting them would make the stream unanalysable for no benefit:
		// as a pull-only client we care about the media, not about proving the
		// peer replayed our random block.
		SkipHandshakeVerification: true,
	}
	if opts != nil {
		connConfig.RecvTap = opts.RecvTap
		connConfig.Logger = opts.Logger
	}

	beginStage := func(name string) func(error) {
		start := time.Now()
		return func(err error) {
			if opts != nil && opts.OnStage != nil {
				opts.OnStage(PlayStage{Name: name, StartedAt: start, EndedAt: time.Now(), Err: err})
			}
		}
	}
	dialer := &net.Dialer{}
	socket, err := dialer.DialContext(ctx, "tcp", target.Host)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to dial")
	}
	if target.Protocol == "rtmps" {
		finishTLS := beginStage("tls")
		serverName, _, _ := net.SplitHostPort(target.Host)
		config := &tls.Config{ServerName: serverName}
		if opts != nil && opts.TLSConfig != nil {
			config = opts.TLSConfig.Clone()
			if config.ServerName == "" {
				config.ServerName = serverName
			}
		}
		secure := tls.Client(socket, config)
		err = secure.HandshakeContext(ctx)
		finishTLS(err)
		if err != nil {
			_ = socket.Close()
			return nil, nil, nil, errors.Wrap(err, "failed TLS handshake")
		}
		socket = secure
	}

	// Close the socket directly to interrupt a blocked handshake or write.
	// Conn.Close waits for queued writers before closing the transport.
	stopWatching := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = socket.SetDeadline(time.Now())
			_ = socket.Close()
		case <-stopWatching:
		}
	}()

	finishHandshake := beginStage("handshake")
	cc, err := newClientConnWithSetup(socket, connConfig)
	finishHandshake(err)
	if err != nil {
		close(stopWatching)
		return nil, nil, nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			close(stopWatching)
			_ = socket.SetDeadline(time.Now())
			_ = socket.Close()
			_ = cc.Close()
			<-cc.Done()
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
	finishConnect := beginStage("app_connect")
	err = cc.ConnectContext(ctx, connect)
	finishConnect(err)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to connect")
	}

	chunkSize := uint32(0)
	start := int64(-2)
	if opts != nil {
		chunkSize = opts.ChunkSize
		start = opts.Start
	}

	finishCreate := beginStage("create_stream")
	stream, err := cc.CreateStreamContext(ctx, nil, chunkSize)
	finishCreate(err)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to create stream")
	}

	finishPlay := beginStage("play")
	if err := stream.Play(&message.NetStreamPlay{
		StreamName: target.StreamName,
		Start:      start,
	}); err != nil {
		finishPlay(err)
		return nil, nil, nil, errors.Wrap(err, "failed to send play command")
	}

	err = stream.waitPlayResult(ctx)
	finishPlay(err)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to start play")
	}

	go func() {
		defer close(stopWatching)
		select {
		case <-ctx.Done():
		case <-cc.Done():
		}
		_ = cc.Close()
		stream.assumeClosed()
	}()

	cleanup = false
	return cc, stream, target, nil
}
