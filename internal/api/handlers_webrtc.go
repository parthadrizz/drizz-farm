package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
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

	// Register H.264 codec
	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    "video/H264",
			ClockRate:   90000,
			SDPFmtpLine: "packetization-mode=1;profile-level-id=42e01f;level-asymmetry-allowed=1",
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo)

	// PLI interceptor
	ir := &interceptor.Registry{}
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
		webrtc.RTPCodecCapability{MimeType: "video/H264"},
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

	// Read RTCP
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(buf); err != nil {
				return
			}
		}
	}()

	// Set offer
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

	// Wait for ICE
	<-webrtc.GatheringCompletePromise(pc)

	// Start H.264 streaming
	go h.streamH264(pc, videoTrack, serial)

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Info().Str("state", state.String()).Str("serial", serial).Msg("webrtc: state")
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			pc.Close()
		}
	})

	JSON(w, 200, map[string]string{
		"sdp":  pc.LocalDescription().SDP,
		"type": pc.LocalDescription().Type.String(),
	})
}

func (h *webrtcHandlers) streamH264(pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticSample, serial string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateDisconnected {
			cancel()
		}
	})

	// Cache SPS/PPS for resending on PLI
	var cachedSPS, cachedPPS []byte

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cmd := exec.CommandContext(ctx, h.sdk.ADBPath(), "-s", serial, "shell",
			"screenrecord", "--output-format=h264", "--size", "720x1600", "--bit-rate", "4000000", "-")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			return
		}

		log.Info().Str("serial", serial).Msg("webrtc: screenrecord started")

		h.readNALsAndSend(ctx, stdout, track, &cachedSPS, &cachedPPS)

		cmd.Process.Kill()
		cmd.Wait()

		select {
		case <-ctx.Done():
			return
		default:
			log.Debug().Msg("webrtc: restarting screenrecord")
		}
	}
}

// readNALsAndSend reads Annex B H.264, strips start codes, builds access units, sends samples.
func (h *webrtcHandlers) readNALsAndSend(ctx context.Context, reader io.Reader, track *webrtc.TrackLocalStaticSample, sps, pps *[]byte) {
	buf := make([]byte, 0, 512*1024)
	tmp := make([]byte, 65536)

	// Accumulate NALs into access units (one frame = SPS+PPS+IDR or just slice NALs)
	var accessUnit []byte
	lastSend := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)

			// Extract complete NAL units
			for {
				nalStart := findStartCode(buf, 0)
				if nalStart < 0 {
					break
				}
				nalEnd := findStartCode(buf, nalStart+4)
				if nalEnd < 0 {
					break
				}

				// Extract NAL WITHOUT start code (strip 00 00 00 01)
				nalData := buf[nalStart+4 : nalEnd]
				buf = buf[nalEnd:]

				if len(nalData) == 0 {
					continue
				}

				nalType := nalData[0] & 0x1F

				switch nalType {
				case 7: // SPS
					*sps = make([]byte, len(nalData))
					copy(*sps, nalData)
				case 8: // PPS
					*pps = make([]byte, len(nalData))
					copy(*pps, nalData)
				case 5: // IDR (keyframe) — prepend SPS+PPS in Annex B
					// Pion expects Annex B format with start codes
					accessUnit = accessUnit[:0]
					if *sps != nil && *pps != nil {
						accessUnit = append(accessUnit, 0, 0, 0, 1)
						accessUnit = append(accessUnit, *sps...)
						accessUnit = append(accessUnit, 0, 0, 0, 1)
						accessUnit = append(accessUnit, *pps...)
					}
					accessUnit = append(accessUnit, 0, 0, 0, 1)
					accessUnit = append(accessUnit, nalData...)

					now := time.Now()
					duration := now.Sub(lastSend)
					if duration < time.Millisecond {
						duration = 33 * time.Millisecond
					}
					lastSend = now

					if err := track.WriteSample(media.Sample{
						Data:     accessUnit,
						Duration: duration,
					}); err != nil {
						return
					}
					accessUnit = accessUnit[:0]

				case 1: // Non-IDR slice (P/B frame)
					// Send raw NAL in Annex B
					frame := make([]byte, 4+len(nalData))
					frame[0] = 0; frame[1] = 0; frame[2] = 0; frame[3] = 1
					copy(frame[4:], nalData)

					now := time.Now()
					duration := now.Sub(lastSend)
					if duration < time.Millisecond {
						duration = 33 * time.Millisecond
					}
					lastSend = now

					if err := track.WriteSample(media.Sample{
						Data:     frame,
						Duration: duration,
					}); err != nil {
						return
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func findStartCode(data []byte, offset int) int {
	for i := offset; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			return i
		}
	}
	return -1
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
