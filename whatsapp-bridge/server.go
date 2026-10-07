package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"go.mau.fi/whatsmeow"
)

// The API listens on 127.0.0.1 ONLY. Anyone who can reach it can send
// messages as you, so it must never listen on 0.0.0.0 or any other address.
const listenHost = "127.0.0.1"

// loggedOut is set when WhatsApp tells us this linked device was removed.
// It is the only state that means "scan a new QR code".
var loggedOut atomic.Bool

type SendMessageResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	MessageID string `json:"message_id,omitempty"`
}

type SendMessageRequest struct {
	Recipient string `json:"recipient"`
	Message   string `json:"message"`
	MediaPath string `json:"media_path,omitempty"`
	// DeliberateResend must be true to send identical content to the same
	// chat again within 24 hours (the Repeat lock).
	DeliberateResend bool `json:"deliberate_resend,omitempty"`
}

type DownloadMediaRequest struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
}

type DownloadMediaResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	Filename string `json:"filename,omitempty"`
	Path     string `json:"path,omitempty"`
}

type HealthResponse struct {
	Connected bool   `json:"connected"`
	LoggedIn  bool   `json:"logged_in"`
	Status    string `json:"status"` // ok | connecting | logged_out
	TestLock  bool   `json:"test_lock"`
}

func healthStatus(client *whatsmeow.Client) HealthResponse {
	h := HealthResponse{
		Connected: client.IsConnected(),
		LoggedIn:  client.Store.ID != nil && !loggedOut.Load() && client.IsLoggedIn(),
		TestLock:  testLockOn(),
	}
	switch {
	case client.Store.ID == nil || loggedOut.Load():
		h.Status = "logged_out" // needs a new QR scan
	case h.Connected && h.LoggedIn:
		h.Status = "ok"
	default:
		h.Status = "connecting" // temporary, will recover by itself
	}
	return h
}

// guard wraps a handler with two defences against a malicious web page in
// your browser quietly calling the local API:
//   - the Host header must be a loopback name (blocks DNS rebinding)
//   - POST bodies must be application/json (a web page cannot send that to
//     another origin without a CORS preflight, which this server never allows)
func guard(next http.HandlerFunc, post bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if post {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		} else if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func startRESTServer(client *whatsmeow.Client, messageStore *MessageStore, sender *Sender, port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(healthStatus(client))
	}, false))

	mux.HandleFunc("/api/send", guard(func(w http.ResponseWriter, r *http.Request) {
		var req SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if req.Recipient == "" {
			http.Error(w, "Recipient is required", http.StatusBadRequest)
			return
		}
		if req.Message == "" && req.MediaPath == "" {
			http.Error(w, "Message or media path is required", http.StatusBadRequest)
			return
		}

		res := sender.Send(req.Recipient, req.Message, req.MediaPath, req.DeliberateResend)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.Code)
		json.NewEncoder(w).Encode(SendMessageResponse{Success: res.Success, Message: res.Message, MessageID: res.MessageID})
	}, true))

	mux.HandleFunc("/api/download", guard(func(w http.ResponseWriter, r *http.Request) {
		var req DownloadMediaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if req.MessageID == "" || req.ChatJID == "" {
			http.Error(w, "Message ID and Chat JID are required", http.StatusBadRequest)
			return
		}

		success, mediaType, filename, path, err := downloadMedia(client, messageStore, req.MessageID, req.ChatJID)

		w.Header().Set("Content-Type", "application/json")
		if !success || err != nil {
			errMsg := "Unknown error"
			if err != nil {
				errMsg = err.Error()
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(DownloadMediaResponse{Success: false, Message: fmt.Sprintf("Failed to download media: %s", errMsg)})
			return
		}
		json.NewEncoder(w).Encode(DownloadMediaResponse{
			Success: true, Message: fmt.Sprintf("Successfully downloaded %s media", mediaType),
			Filename: filename, Path: path,
		})
	}, true))

	addr := fmt.Sprintf("%s:%d", listenHost, port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		appLog("REST API server could not start on %s: %v", addr, err)
		return
	}
	appLog("REST API listening on %s (local only)", addr)
	go func() {
		if err := http.Serve(ln, mux); err != nil {
			appLog("REST API server stopped: %v", err)
		}
	}()
}
