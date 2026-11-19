package onvif

import (
	"net"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
)

type UDPSourceFaultStatus struct {
	NegotiatedPort   int       `json:"negotiated_port"`
	AlternatePort    int       `json:"alternate_port"`
	ReboundAt        time.Time `json:"rebound_at,omitzero"`
	RestoredAt       time.Time `json:"restored_at,omitzero"`
	AlternatePackets uint64    `json:"alternate_packets"`
}

type udpSourceFault struct {
	*net.UDPConn
	alternate    *net.UDPConn
	duration     time.Duration
	mu           sync.Mutex
	routes       map[*gortsplib.ServerSession]string
	selected     *gortsplib.ServerSession
	target       string
	start, until time.Time
	status       UDPSourceFaultStatus
}

func openUDPSourceFault(primary *net.UDPConn, ip net.IP, duration time.Duration) (*udpSourceFault, error) {
	alternate, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	if err != nil {
		return nil, err
	}
	return &udpSourceFault{UDPConn: primary, alternate: alternate, duration: duration, routes: make(map[*gortsplib.ServerSession]string),
		status: UDPSourceFaultStatus{NegotiatedPort: primary.LocalAddr().(*net.UDPAddr).Port, AlternatePort: alternate.LocalAddr().(*net.UDPAddr).Port}}, nil
}

func (f *udpSourceFault) setup(session *gortsplib.ServerSession, ip net.IP, port int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[session] = (&net.UDPAddr{IP: ip, Port: port}).String()
}

func (f *udpSourceFault) arm(session *gortsplib.ServerSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if target := f.routes[session]; target != "" && f.start.IsZero() {
		f.selected, f.target = session, target
		f.start = time.Now().Add(2 * time.Second)
		f.until = f.start.Add(f.duration)
	}
}

func (f *udpSourceFault) forget(session *gortsplib.ServerSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.routes, session)
	if f.selected == session {
		f.selected, f.target = nil, ""
	}
}

func (f *udpSourceFault) snapshot() UDPSourceFaultStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *udpSourceFault) WriteTo(data []byte, address net.Addr) (int, error) {
	now := time.Now()
	f.mu.Lock()
	selected := f.target != "" && f.target == address.String() && !now.Before(f.start)
	shifted := selected && now.Before(f.until)
	f.mu.Unlock()
	socket := f.UDPConn
	if shifted {
		socket = f.alternate
		_ = socket.SetWriteDeadline(now.Add(time.Second))
	}
	n, err := socket.WriteTo(data, address)
	if err == nil && selected {
		f.mu.Lock()
		if shifted {
			f.status.AlternatePackets++
			if f.status.ReboundAt.IsZero() {
				f.status.ReboundAt = now
			}
		} else if f.status.AlternatePackets > 0 && f.status.RestoredAt.IsZero() {
			f.status.RestoredAt = now
		}
		f.mu.Unlock()
	}
	return n, err
}
