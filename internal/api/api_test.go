package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

func TestJSON(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, http.StatusOK, map[string]string{"hello": "world"})

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json, got %s", w.Header().Get("Content-Type"))
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["hello"] != "world" {
		t.Errorf("expected 'world', got '%s'", result["hello"])
	}
}

func TestJSONNil(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, http.StatusNoContent, nil)
	if w.Code != 204 {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestErrorMappingPoolExhausted(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, pool.ErrPoolExhausted)
	if w.Code != 503 {
		t.Errorf("expected 503, got %d", w.Code)
	}
	var resp ErrorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error != "pool_exhausted" {
		t.Errorf("expected pool_exhausted, got %s", resp.Error)
	}
}

func TestErrorMappingSessionNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, fmt.Errorf("%w: abc", session.ErrSessionNotFound))
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestErrorMappingQueueFull(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, session.ErrQueueFull)
	if w.Code != 503 {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestErrorMappingQueueTimeout(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, session.ErrQueueTimeout)
	if w.Code != 504 {
		t.Errorf("expected 504, got %d", w.Code)
	}
}

func TestErrorMappingGeneric(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, fmt.Errorf("something went wrong"))
	if w.Code != 500 {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestMiddlewareRequestID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header")
	}
}

func TestMiddlewareRequestIDPreserved(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "my-id")
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") != "my-id" {
		t.Errorf("expected 'my-id', got '%s'", w.Header().Get("X-Request-ID"))
	}
}

func TestMiddlewareCORS(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS allow origin *")
	}
}

func TestMiddlewareCORSPreflight(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/", nil)
	handler.ServeHTTP(w, r)

	if w.Code != 204 {
		t.Errorf("expected 204 for OPTIONS, got %d", w.Code)
	}
}

func TestMiddlewareRecovery(t *testing.T) {
	handler := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if w.Code != 500 {
		t.Errorf("expected 500 after panic, got %d", w.Code)
	}
}

func TestStatusWriterCapturesCode(t *testing.T) {
	w := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: w, status: 200}
	sw.WriteHeader(404)
	if sw.status != 404 {
		t.Errorf("expected 404, got %d", sw.status)
	}
}
