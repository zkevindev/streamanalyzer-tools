package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"streamanalyzer/internal/rtmpraw"
	"streamanalyzer/internal/pcaprtmp"

	"github.com/google/gopacket/layers"
)

func main() {
	pcapPath := flag.String("pcap", "example/parse_rtmp_form_wireshark/11935.pcap", "Wireshark pcap input")
	serverPort := flag.Uint("serverPort", 1935, "RTMP server port (tcp)")
	outDir := flag.String("outDir", "example/parse_rtmp_form_wireshark/out", "output directory")
	skip := flag.Int("skip", rtmpraw.DefaultHandshakeSkipBytes, "bytes to skip before RTMP chunk parsing (handshake size)")
	flag.Parse()

	if err := run(*pcapPath, layers.TCPPort(*serverPort), *outDir, *skip); err != nil {
		fmt.Fprintf(os.Stderr, "wireshark parse failed: %v\n", err)
		os.Exit(1)
	}
}

func run(pcapPath string, serverPort layers.TCPPort, outDir string, skip int) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	flows, err := pcaprtmp.ExtractFlowsFromPCAP(pcapPath, pcaprtmp.DefaultOptions(serverPort))
	if err != nil {
		return err
	}
	if len(flows) == 0 {
		fmt.Println("no candidate RTMP flows found (missing SYN or too little payload)")
		return nil
	}

	fmt.Printf("candidate flows: %d\n", len(flows))

	type report struct {
		flowID int
		meta   pcaprtmp.FlowMeta

		clientToServerBytes int
		serverToClientBytes int

		dir       pcaprtmp.Direction
		dumpRawDir string

		rawPath   string
		videoPath string
		audioPath string

		videoCodec string
		parseErr   string
	}

	reports := make([]report, 0, len(flows))
	for i, f := range flows {
		flowID := i + 1
		r := report{
			flowID: flowID,
			meta:   f.Meta,
			clientToServerBytes: len(f.ClientToServer),
			serverToClientBytes: len(f.ServerToClient),
		}

		dir := pcaprtmp.DetectDirection(f, skip, 64*1024)
		r.dir = dir

		var raw []byte
		var rawDir string
		switch dir {
		case pcaprtmp.DirectionPull:
			raw = f.ServerToClient
			rawDir = "server->client"
		case pcaprtmp.DirectionPush:
			raw = f.ClientToServer
			rawDir = "client->server"
		default:
			reports = append(reports, r)
			continue
		}
		r.dumpRawDir = rawDir

		dirOut := filepath.Join(outDir, string(dir))
		if err := os.MkdirAll(dirOut, 0o755); err != nil {
			return err
		}

		r.rawPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_%s.raw", flowID, rawDir))
		if err := os.WriteFile(r.rawPath, raw, 0o644); err != nil {
			return err
		}

		r.videoPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_video.annexb", flowID))
		r.audioPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_audio.adts.aac", flowID))
		videoF, err := os.Create(r.videoPath)
		if err != nil {
			return err
		}
		audioF, err := os.Create(r.audioPath)
		if err != nil {
			_ = videoF.Close()
			return err
		}

		res, parseErr := rtmpraw.ParseRTMPRaw(bytes.NewReader(raw), rtmpraw.Options{SkipBytes: skip}, rtmpraw.Output{
			VideoWriter: videoF,
			AudioWriter: audioF,
		})

		_ = videoF.Close()
		_ = audioF.Close()
		if parseErr != nil {
			r.parseErr = parseErr.Error()
		} else {
			r.videoCodec = res.VideoCodec
		}

		reports = append(reports, r)
	}

	fmt.Printf("\n=== RTMP Wireshark Summary ===\n")
	for _, r := range reports {
		errPart := "-"
		if r.parseErr != "" {
			errPart = r.parseErr
		}

		fmt.Printf(
			"flow%03d SYN=%v (SYNcnt=%d) pkts=%d payloadBytes=%d client=%s:%d server=%s:%d dir=%s dump=%s rawLen=%d codec=%s err=%s video=%s audio=%s\n",
			r.flowID,
			r.meta.HasSYN,
			r.meta.SYNCount,
			r.meta.TCPPktCount,
			r.meta.PayloadBytes,
			r.meta.ClientIP, r.meta.ClientPort,
			r.meta.ServerIP, r.meta.ServerPort,
			r.dir,
			r.dumpRawDir,
			func() int {
				if r.dumpRawDir == "client->server" {
					return r.clientToServerBytes
				}
				if r.dumpRawDir == "server->client" {
					return r.serverToClientBytes
				}
				return 0
			}(),
			r.videoCodec,
			errPart,
			r.videoPath,
			r.audioPath,
		)
	}

	return nil
}
