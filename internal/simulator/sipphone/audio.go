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
	conn     *net.UDPConn
	local    netip.AddrPort
	sent     atomic.Uint64
	received atomic.Uint64

	mu      sync.Mutex
	samples []int16
	next    int
}

func openAudio(ip netip.Addr) (*audioSession, error) {
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 0)))
	if err != nil {
		return nil, err
	}
	return &audioSession{conn: conn, local: conn.LocalAddr().(*net.UDPAddr).AddrPort()}, nil
}

func (a *audioSession) run(ctx context.Context, params audioParameters) func() {
	ctx, cancel := context.WithCancel(ctx)
	var media sync.WaitGroup
	if params.send {
		media.Go(func() { a.sendTone(ctx, params) })
	}
	if params.recv {
		media.Go(func() { a.receive(params) })
	}
	return func() {
		cancel()
		a.conn.Close()
		media.Wait()
	}
}

func (a *audioSession) sendTone(ctx context.Context, params audioParameters) {
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
	header := rtp.Header{Version: 2, Marker: true, PayloadType: params.codec.pt,
		SSRC: rand.Uint32(), Timestamp: rand.Uint32(), SequenceNumber: uint16(rand.Uint32())}
	dist := params.remote
	buffer := make([]byte, 12+audioFrame)
	offset := 0
	for ctx.Err() == nil {
		packet := rtp.Packet{Header: header, Payload: tone[offset : offset+audioFrame]}
		n, err := packet.MarshalTo(buffer)
		if err != nil {
			return
		}
		if _, err := a.conn.WriteToUDPAddrPort(buffer[:n], dist); err == nil {
			a.sent.Add(1)
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

func (a *audioSession) receive(params audioParameters) {
	decoder := g711.DecodeUlawFrame
	if params.codec.name == "PCMA" {
		decoder = g711.DecodeAlawFrame
	}
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
		if packet.Unmarshal(buf[:n]) != nil || packet.Version != 2 || packet.PayloadType != params.codec.pt || len(packet.Payload) == 0 {
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
		a.received.Add(1)
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
