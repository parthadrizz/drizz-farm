package api

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/pool"
)

type webrtcHandlers struct {
	pool *pool.Pool
	sdk  *android.SDK
	mu   sync.Mutex
}

func (h *webrtcHandlers) Offer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	serial := h.findSerial(id)
	if serial == "" {
		JSON(w, 404, ErrorResponse{Error: "not_found", Message: "instance not found", Code: 404})
		return
	}

	var req struct {
		SDP  string `json:"sdp"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSON(w, 400, ErrorResponse{Error: "invalid", Message: err.Error(), Code: 400})
		return
	}

	// Use default codecs (H264 + VP8 + VP9 + Opus etc.) — lets browser negotiate
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		JSON(w, 500, ErrorResponse{Error: "codec_init", Message: err.Error(), Code: 500})
		return
	}

	// Default interceptors (NACK, RTCP reports) + PLI
	ir := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, ir); err != nil {
		JSON(w, 500, ErrorResponse{Error: "interceptor_init", Message: err.Error(), Code: 500})
		return
	}
	pliFactory, _ := intervalpli.NewReceiverInterceptor(intervalpli.GeneratorInterval(2 * time.Second))
	if pliFactory != nil {
		ir.Add(pliFactory)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir))

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "pc_failed", Message: err.Error(), Code: 500})
		return
	}

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "drizz-farm",
	)
	if err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "track_failed", Message: err.Error(), Code: 500})
		return
	}

	rtpSender, err := pc.AddTrack(videoTrack)
	if err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "add_track", Message: err.Error(), Code: 500})
		return
	}

	// PLI channel — browser requests keyframe
	pliCh := make(chan struct{}, 1)

	// Read RTCP, detect PLI/FIR
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := rtpSender.Read(buf)
			if err != nil {
				return
			}
			pkts, _ := rtcp.Unmarshal(buf[:n])
			for _, pkt := range pkts {
				switch pkt.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					select {
					case pliCh <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	// Set remote offer
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}); err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "set_offer", Message: err.Error(), Code: 500})
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "create_answer", Message: err.Error(), Code: 500})
		return
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "set_answer", Message: err.Error(), Code: 500})
		return
	}

	// Wait for ICE gathering
	<-webrtc.GatheringCompletePromise(pc)

	// Single OnConnectionStateChange — handles both logging and lifecycle
	connectedCh := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Info().Str("state", state.String()).Str("serial", serial).Msg("webrtc: state")
		switch state {
		case webrtc.PeerConnectionStateConnected:
			select {
			case connectedCh <- struct{}{}:
			default:
			}
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			cancel()
			pc.Close()
		case webrtc.PeerConnectionStateDisconnected:
			// Temporary — ICE can recover. Don't kill the stream.
			log.Warn().Str("serial", serial).Msg("webrtc: ICE disconnected (may recover)")
		}
	})

	// Start streaming — waits for connection before sending frames
	go h.streamH264(ctx, cancel, videoTrack, serial, pliCh, connectedCh)

	JSON(w, 200, map[string]string{
		"sdp":  pc.LocalDescription().SDP,
		"type": pc.LocalDescription().Type.String(),
	})
}

func (h *webrtcHandlers) streamH264(ctx context.Context, cancel context.CancelFunc, track *webrtc.TrackLocalStaticSample, serial string, pliCh <-chan struct{}, connectedCh <-chan struct{}) {
	defer cancel()

	// Wait for WebRTC connection
	select {
	case <-connectedCh:
		log.Info().Str("serial", serial).Msg("webrtc: connection ready, starting stream")
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
		log.Warn().Str("serial", serial).Msg("webrtc: connection timeout")
		return
	}

	// Try scrcpy in a loop (restart on crash/timeout)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if h.streamViaScrcpy(ctx, track, serial, pliCh) {
			// scrcpy worked but stream ended — restart
			select {
			case <-ctx.Done():
				return
			default:
				log.Info().Str("serial", serial).Msg("webrtc: scrcpy ended, restarting in 1s")
				time.Sleep(1 * time.Second)
				continue
			}
		}

		// scrcpy failed to start — fall back to screenrecord
		log.Info().Str("serial", serial).Msg("webrtc: scrcpy unavailable, falling back to screenrecord")
		h.streamViaScreenrecord(ctx, track, serial, pliCh)
		return
	}
}

// readScrcpyStream parses scrcpy's length-prefixed H.264 stream (no start codes).
// SPS/PPS are sent as zero-duration samples before the next VCL or after a PLI.
func (h *webrtcHandlers) readScrcpyStream(ctx context.Context, conn net.Conn, track *webrtc.TrackLocalStaticSample, pliCh <-chan struct{}) {
	reader := bufio.NewReader(conn)

	var sps, pps []byte
	var needConfig atomic.Bool
	needConfig.Store(true)
	frameCount := 0
	frameDuration := 33 * time.Millisecond

	for {
		// Handle cancellation / PLI
		select {
		case <-ctx.Done():
			return
		case <-pliCh:
			needConfig.Store(true)
		default:
		}

		// Read length prefix
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 {
			continue
		}

		nal := make([]byte, n)
		if _, err := io.ReadFull(reader, nal); err != nil {
			return
		}
		if len(nal) == 0 {
			continue
		}

		nalType := nal[0] & 0x1F

		switch nalType {
		case 7: // SPS
			sps = append([]byte{}, nal...)
			continue
		case 8: // PPS
			pps = append([]byte{}, nal...)
			continue
		case 6, 9: // SEI, AUD
			continue
		}

		// Send SPS/PPS if needed (PLI or before keyframe)
		if needConfig.Load() && sps != nil && pps != nil && (nalType == 5 || nalType == 1) {
			if err := track.WriteSample(media.Sample{Data: sps, Duration: 0}); err != nil {
				return
			}
			if err := track.WriteSample(media.Sample{Data: pps, Duration: 0}); err != nil {
				return
			}
			needConfig.Store(false)
		}

		// Send VCL
		if nalType == 5 || nalType == 1 {
			if err := track.WriteSample(media.Sample{Data: nal, Duration: frameDuration}); err != nil {
				return
			}
			frameCount++
			if frameCount == 1 && nalType == 5 {
				log.Info().Int("sps_len", len(sps)).Int("pps_len", len(pps)).Int("idr_len", len(nal)).Msg("webrtc: scrcpy first frame")
			}
			continue
		}
	}
}

// streamViaScrcpy pushes scrcpy-server to device and reads H.264 over TCP.
// Continuous capture — no stalls on static screens.
func (h *webrtcHandlers) streamViaScrcpy(ctx context.Context, track *webrtc.TrackLocalStaticSample, serial string, pliCh <-chan struct{}) bool {
	// Find scrcpy-server
	scrcpyPath := ""
	for _, p := range []string{
		"/opt/homebrew/Cellar/scrcpy/3.2/share/scrcpy/scrcpy-server",
		"/opt/homebrew/share/scrcpy/scrcpy-server",
		"/usr/local/share/scrcpy/scrcpy-server",
		"/usr/share/scrcpy/scrcpy-server",
	} {
		if _, err := os.Stat(p); err == nil {
			scrcpyPath = p
			break
		}
	}
	if scrcpyPath == "" {
		log.Debug().Msg("webrtc: scrcpy-server not found")
		return false
	}

	// Push server to device
	pushCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "push", scrcpyPath, "/data/local/tmp/scrcpy-server.jar")
	if err := pushCmd.Run(); err != nil {
		log.Warn().Err(err).Msg("webrtc: scrcpy push failed")
		return false
	}

	// Find free port and forward
	port, err := getFreePort()
	if err != nil {
		return false
	}

	fwdCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "forward", fmt.Sprintf("tcp:%d", port), "localabstract:scrcpy")
	if err := fwdCmd.Run(); err != nil {
		log.Warn().Err(err).Msg("webrtc: scrcpy forward failed")
		return false
	}
	defer exec.Command(h.sdk.ADBPath(), "-s", serial, "forward", "--remove", fmt.Sprintf("tcp:%d", port)).Run()

	// Start scrcpy-server on device
	serverCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
		"CLASSPATH=/data/local/tmp/scrcpy-server.jar",
		"app_process", "/", "com.genymobile.scrcpy.Server",
		"3.2",
		"tunnel_forward=true",
		"video=true", "audio=false", "control=false",
		"max_size=0", "video_bit_rate=3000000", "max_fps=10",
		"video_codec=h264", "send_frame_meta=false",
		"send_device_meta=false", "send_dummy_byte=false",
	)
	if err := serverCmd.Start(); err != nil {
		log.Warn().Err(err).Msg("webrtc: scrcpy server start failed")
		return false
	}
	defer serverCmd.Process.Kill()

	// Wait for server to start and connect
	time.Sleep(2 * time.Second)

	videoConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		log.Warn().Err(err).Msg("webrtc: scrcpy connect failed")
		return false
	}
	defer videoConn.Close()

	// Skip any initial handshake/header bytes from scrcpy
	videoConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	dummy := make([]byte, 128)
	if n, _ := videoConn.Read(dummy); n > 0 {
		log.Debug().Int("bytes", n).Msg("webrtc: skipped scrcpy header")
	}
	videoConn.SetReadDeadline(time.Time{}) // clear deadline

	log.Info().Str("serial", serial).Int("port", port).Msg("webrtc: scrcpy streaming started")

	// Read H.264 from scrcpy TCP socket and send via WebRTC
	h.readAndSendNALs(ctx, videoConn, track, pliCh)

	log.Info().Str("serial", serial).Msg("webrtc: scrcpy streaming ended")
	return true
}

// streamViaScreenrecord uses Android's built-in screenrecord (fallback).
func (h *webrtcHandlers) streamViaScreenrecord(ctx context.Context, track *webrtc.TrackLocalStaticSample, serial string, pliCh <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		checkCmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell", "echo", "ok")
		if out, err := checkCmd.Output(); err != nil || len(out) == 0 {
			time.Sleep(3 * time.Second)
			continue
		}

		cmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
			"screenrecord", "--output-format=h264", "--bit-rate", "4000000", "--time-limit", "120", "-")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		if err := cmd.Start(); err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		log.Info().Str("serial", serial).Msg("webrtc: screenrecord started (fallback)")

		go func() {
			time.Sleep(300 * time.Millisecond)
			exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell", "input", "swipe", "1", "1", "1", "1", "50").Run()
		}()

		h.readAndSendNALs(ctx, stdout, track, pliCh)

		cmd.Process.Kill()
		cmd.Wait()

		select {
		case <-ctx.Done():
			return
		default:
			time.Sleep(2 * time.Second)
		}
	}
}

// readAndSendNALs reads H.264 NALs and sends them via WebRTC.
// Uses a producer/consumer pattern: reader goroutine reads NALs as fast as
// possible into a channel, sender goroutine sends the latest frames.
// Old frames are dropped to prevent the stream from falling behind.
func (h *webrtcHandlers) readAndSendNALs(ctx context.Context, reader io.Reader, track *webrtc.TrackLocalStaticSample, pliCh <-chan struct{}) {
	r, err := h264reader.NewReader(reader)
	if err != nil {
		return
	}

	type nalUnit struct {
		data    []byte
		nalType byte
	}

	// Small buffer — forces drops when consumer can't keep up
	nalCh := make(chan nalUnit, 2)

	// Producer: reads NALs as fast as possible
	go func() {
		defer close(nalCh)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			nal, err := r.NextNAL()
			if err != nil {
				return
			}
			if len(nal.Data) == 0 {
				continue
			}
			nu := nalUnit{
				data:    append([]byte{}, nal.Data...),
				nalType: nal.Data[0] & 0x1F,
			}
			// Non-blocking send — drop if channel full (consumer behind)
			select {
			case nalCh <- nu:
			default:
				// Drop this NAL — we're behind
			}
		}
	}()

	var sps, pps []byte
	var needConfig atomic.Bool
	needConfig.Store(true)
	frameCount := 0
	frameDuration := time.Millisecond * 33

	for {
		var nu nalUnit
		var ok bool

		select {
		case <-ctx.Done():
			return
		case <-pliCh:
			needConfig.Store(true)
		case nu, ok = <-nalCh:
			if !ok {
				log.Info().Int("frames_sent", frameCount).Msg("webrtc: stream ended")
				return
			}
		}

		if len(nu.data) == 0 {
			continue
		}

		switch nu.nalType {
		case 7: // SPS
			sps = nu.data
		case 8: // PPS
			pps = nu.data

		case 5: // IDR — always send with SPS+PPS
			var au []byte
			if sps != nil {
				au = append(au, 0, 0, 0, 1)
				au = append(au, sps...)
			}
			if pps != nil {
				au = append(au, 0, 0, 0, 1)
				au = append(au, pps...)
			}
			au = append(au, 0, 0, 0, 1)
			au = append(au, nu.data...)
			needConfig.Store(false)

			if err := track.WriteSample(media.Sample{Data: au, Duration: frameDuration}); err != nil {
				return
			}
			frameCount++
			if frameCount == 1 {
				log.Info().Int("sps_len", len(sps)).Int("pps_len", len(pps)).Int("idr_len", len(nu.data)).Msg("webrtc: first IDR sent")
			}

		case 1: // P/B frame
			var frame []byte
			if needConfig.Load() && sps != nil && pps != nil {
				frame = append(frame, 0, 0, 0, 1)
				frame = append(frame, sps...)
				frame = append(frame, 0, 0, 0, 1)
				frame = append(frame, pps...)
				needConfig.Store(false)
			}
			frame = append(frame, 0, 0, 0, 1)
			frame = append(frame, nu.data...)

			if err := track.WriteSample(media.Sample{Data: frame, Duration: frameDuration}); err != nil {
				return
			}
			frameCount++

		case 6, 9: // SEI, AUD — skip
			continue
		}

		if frameCount > 0 && frameCount%300 == 0 {
			log.Debug().Int("frames", frameCount).Msg("webrtc: streaming")
		}
	}
}

func (h *webrtcHandlers) findSerial(id string) string {
	if h.pool == nil {
		return ""
	}
	for _, inst := range h.pool.Status().Instances {
		if inst.ID == id || inst.SessionID == id {
			return inst.Serial
		}
	}
	if inst, ok := h.pool.GetInstance(id); ok && inst.Device != nil {
		return inst.Device.Serial()
	}
	return ""
}
