package sipphone

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

const (
	sampleRate  = 8000
	audioFrame  = 160
	clipSamples = 20 * sampleRate
)

type audioSession struct {
	earlySent     rtpStats
	earlyReceived rtpStats
	digits        chan uint8
	digitEvents   []DigitEvent
	nextHeader    rtp.Header
	conn          *net.UDPConn
	local         netip.AddrPort
	sent          rtpStats
	received      rtpStats
	fault         atomic.Uint32

	mu      sync.Mutex
	samples []int16
	next    int
}

func openAudio(ip netip.Addr) (*audioSession, error) {
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 0)))
	if err != nil {
		return nil, err
	}
	return &audioSession{conn: conn, digits: make(chan uint8, 8), local: conn.LocalAddr().(*net.UDPAddr).AddrPort()}, nil
}

func (a *audioSession) run(ctx context.Context, params audioParameters, early bool) func() {
	ctx, cancel := context.WithCancel(ctx)
	var media sync.WaitGroup
	if params.send {
		media.Go(func() { a.sendTone(ctx, params, early) })
	}
	if params.recv {
		media.Go(func() { a.receive(params, early) })
	}
	return sync.OnceFunc(func() {
		cancel()
		if params.recv {
			_ = a.conn.SetReadDeadline(time.Now())
		}
		media.Wait()
		if params.recv {
			_ = a.conn.SetReadDeadline(time.Time{})
		}
	})
}

func (a *audioSession) sendTone(ctx context.Context, params audioParameters, early bool) {
	tone := make([]byte, 800) // 100 ms, exactly 44 periods of 440 Hz.
	encode := g711.EncodeUlawFrame
	if params.codec.name == "PCMA" {
		encode = g711.EncodeAlawFrame
	}
	for i := range tone {
		tone[i] = encode(int16(4800 * math.Sin(2*math.Pi*440*float64(i)/sampleRate)))
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	if a.nextHeader.Version == 0 {
		a.nextHeader = rtp.Header{Version: 2, Marker: true, SSRC: rand.Uint32(), Timestamp: rand.Uint32(), SequenceNumber: uint16(rand.Uint32())}
	}
	header := a.nextHeader
	header.PayloadType = params.codec.pt
	defer func() { a.nextHeader = header }()
	stats := &a.sent
	if early {
		stats = &a.earlySent
	}
	var digit *digitSender
	dist := params.remote
	buffer := make([]byte, 12+audioFrame)
	offset := 0
	for ctx.Err() == nil {
		if !early && params.dtmfPT != 0 && digit == nil {
			select {
			case code := <-a.digits:
				digit = &digitSender{code: code, timestamp: header.Timestamp}
			default:
			}
		}
		packet := rtp.Packet{Header: header, Payload: tone[offset : offset+audioFrame]}
		var digitDone bool
		var digitPayload [4]byte
		if digit != nil {
			digitPayload, packet.Marker, digitDone = digit.payload()
			packet.PayloadType, packet.Timestamp, packet.Payload = params.dtmfPT, digit.timestamp, digitPayload[:]
		}
		if audioFault(a.fault.Load()) == audioFaultSend {
			stats.drop()
		} else {
			n, err := packet.MarshalTo(buffer)
			if err != nil {
				return
			}
			if _, err := a.conn.WriteToUDPAddrPort(buffer[:n], dist); err == nil {
				stats.observe(packet.Header, time.Now())
				if digitDone {
					a.recordDigit("send", digit.code)
				}
			}
		}
		if digitDone {
			digit = nil
		}
		header.SequenceNumber++
		header.Timestamp += audioFrame
		header.Marker = false
		offset = (offset + audioFrame) % len(tone)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *audioSession) receive(params audioParameters, early bool) {
	decoder := g711.DecodeUlawFrame
	if params.codec.name == "PCMA" {
		decoder = g711.DecodeAlawFrame
	}
	stats := &a.received
	if early {
		stats = &a.earlyReceived
	}
	type key struct {
		ssrc, timestamp uint32
		code            uint8
	}
	var previous *key
	buf := make([]byte, 2048)
	for {
		n, remote, err := a.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		// Some RTP peers use a different source port from their SDP receive port.
		if remote.Addr().Unmap() != params.remote.Addr() {
			continue
		}
		var packet rtp.Packet
		if packet.Unmarshal(buf[:n]) != nil || packet.Version != 2 || len(packet.Payload) == 0 {
			continue
		}
		event := !early && params.dtmfPT != 0 && packet.PayloadType == params.dtmfPT
		if packet.PayloadType != params.codec.pt && !event {
			continue
		}
		if audioFault(a.fault.Load()) == audioFaultReceive {
			stats.drop()
			continue
		}
		arrival := time.Now()
		if event {
			if len(packet.Payload) >= 4 && packet.Payload[0] < 16 && params.dtmfEvents&(1<<packet.Payload[0]) != 0 {
				current := key{packet.SSRC, packet.Timestamp, packet.Payload[0]}
				if packet.Payload[1]&0x80 != 0 && (previous == nil || *previous != current) {
					a.recordDigit("receive", current.code)
					previous = &current
				}
				stats.observe(packet.Header, arrival)
			}
			continue
		}
		a.mu.Lock()
		for _, sample := range packet.Payload {
			pcm := decoder(sample)
			if len(a.samples) < clipSamples {
				a.samples = append(a.samples, pcm)
			} else {
				a.samples[a.next] = pcm
				a.next = (a.next + 1) % clipSamples
			}
		}
		a.mu.Unlock()
		stats.observe(packet.Header, arrival)
	}
}

func (a *audioSession) wav() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.samples) == 0 {
		return nil
	}
	dataSize := uint32(len(a.samples) * 2)
	wav := make([]byte, 0, 44+dataSize)
	wav = append(wav, "RIFF"...)
	wav = binary.LittleEndian.AppendUint32(wav, dataSize+36)
	wav = append(wav, "WAVEfmt "...)
	wav = binary.LittleEndian.AppendUint32(wav, 16)
	wav = binary.LittleEndian.AppendUint16(wav, 1) // PCM
	wav = binary.LittleEndian.AppendUint16(wav, 1) // Mono
	wav = binary.LittleEndian.AppendUint32(wav, sampleRate)
	wav = binary.LittleEndian.AppendUint32(wav, 2*sampleRate)
	wav = binary.LittleEndian.AppendUint16(wav, 2)
	wav = binary.LittleEndian.AppendUint16(wav, 16)
	wav = append(wav, "data"...)
	wav = binary.LittleEndian.AppendUint32(wav, dataSize)
	for i := range a.samples {
		wav = binary.LittleEndian.AppendUint16(wav, uint16(a.samples[(a.next+i)%len(a.samples)]))
	}
	return wav
}
