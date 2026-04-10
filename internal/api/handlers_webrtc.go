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

// Offer handles POST /api/v1/sessions/:id/webrtc/offer
// Receives browser's SDP offer, returns SDP answer.
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

	// Create WebRTC peer connection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		JSON(w, 500, ErrorResponse{Error: "webrtc_failed", Message: err.Error(), Code: 500})
		return
	}

	// Add H.264 video track
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
		JSON(w, 500, ErrorResponse{Error: "add_track_failed", Message: err.Error(), Code: 500})
		return
	}

	// Read RTCP packets (required)
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(buf); err != nil {
				return
			}
		}
	}()

	// Set remote description (browser's offer)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  req.SDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "set_offer_failed", Message: err.Error(), Code: 500})
		return
	}

	// Create answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "create_answer_failed", Message: err.Error(), Code: 500})
		return
	}

	// Set local description
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		JSON(w, 500, ErrorResponse{Error: "set_answer_failed", Message: err.Error(), Code: 500})
		return
	}

	// Wait for ICE gathering
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// Start streaming H.264 from screenrecord
	go h.streamH264(pc, videoTrack, serial)

	// Handle connection state
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Info().Str("state", state.String()).Str("serial", serial).Msg("webrtc: connection state")
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			pc.Close()
		}
	})

	// Return answer
	JSON(w, 200, map[string]string{
		"sdp":  pc.LocalDescription().SDP,
		"type": pc.LocalDescription().Type.String(),
	})
}

// streamH264 pipes screenrecord H.264 into the WebRTC video track.
func (h *webrtcHandlers) streamH264(pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticSample, serial string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateDisconnected {
			cancel()
		}
	})

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
			log.Debug().Err(err).Msg("webrtc: pipe failed")
			return
		}
		if err := cmd.Start(); err != nil {
			log.Debug().Err(err).Msg("webrtc: screenrecord start failed")
			return
		}

		log.Info().Str("serial", serial).Msg("webrtc: H.264 streaming started")

		// Read H.264 NAL units and write as samples
		h.readAndSendNALUnits(ctx, stdout, track)

		cmd.Process.Kill()
		cmd.Wait()

		// screenrecord has 3-min limit, restart
		select {
		case <-ctx.Done():
			return
		default:
			log.Debug().Msg("webrtc: restarting screenrecord (3-min limit)")
		}
	}
}

// readAndSendNALUnits reads H.264 Annex B stream and sends NAL units as RTP samples.
func (h *webrtcHandlers) readAndSendNALUnits(ctx context.Context, reader io.Reader, track *webrtc.TrackLocalStaticSample) {
	buf := make([]byte, 0, 512*1024) // accumulate buffer
	tmp := make([]byte, 32768)       // read chunk

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)

			// Find and send complete NAL units (delimited by 00 00 00 01)
			for {
				nalStart := findNALStart(buf, 0)
				if nalStart < 0 {
					break
				}
				nalEnd := findNALStart(buf, nalStart+4)
				if nalEnd < 0 {
					break // Incomplete NAL, wait for more data
				}

				nalUnit := buf[nalStart:nalEnd]
				buf = buf[nalEnd:]

				// Send as WebRTC sample
				if err := track.WriteSample(media.Sample{
					Data:     nalUnit,
					Duration: time.Millisecond * 33, // ~30fps
				}); err != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func findNALStart(data []byte, offset int) int {
	for i := offset; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			return i
		}
	}
	return -1
}

func (h *webrtcHandlers) findSerial(id string) string {
	if h.pool == nil { return "" }
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

// ICECandidate handles POST /api/v1/sessions/:id/webrtc/candidate (trickle ICE)
func (h *webrtcHandlers) ICECandidate(w http.ResponseWriter, r *http.Request) {
	// For now, we use full ICE gathering before returning answer
	// Trickle ICE can be added later
	JSON(w, 200, map[string]string{"status": "not_needed"})
}
