package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

const (
	configDir       = "config"
	testLockFile    = "test_lock"
	repeatLockHours = 24
)

// SendResult is what the send path returns to the HTTP layer.
type SendResult struct {
	Code      int // HTTP status
	Success   bool
	Message   string
	MessageID string
}

func refuse(code int, msg string) SendResult { return SendResult{Code: code, Message: msg} }

// Sender owns everything needed to send: the WhatsApp client, the database
// and the two safety locks. One mutex makes check-send-record atomic so two
// simultaneous identical calls cannot both pass the Repeat lock.
type Sender struct {
	client *whatsmeow.Client
	store  *MessageStore
	mu     sync.Mutex
	now    func() time.Time
	// ownUsers returns this account's own IDs (phone number and hidden @lid).
	ownUsers func() []string
	// connected / transmit are indirection points so the locks can be unit
	// tested without a real WhatsApp connection.
	connected func() bool
	transmit  func(ctx context.Context, to types.JID, msg *waProto.Message) (id string, ts time.Time, err error)
	upload    func(ctx context.Context, data []byte, t whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	chatName  func(jid types.JID) string
}

func NewSender(client *whatsmeow.Client, store *MessageStore) *Sender {
	s := &Sender{client: client, store: store, now: time.Now}
	s.ownUsers = func() []string {
		var users []string
		if client.Store.ID != nil {
			users = append(users, client.Store.ID.User)
		}
		if !client.Store.LID.IsEmpty() {
			users = append(users, client.Store.LID.User)
		}
		return users
	}
	s.connected = client.IsConnected
	s.transmit = func(ctx context.Context, to types.JID, msg *waProto.Message) (string, time.Time, error) {
		resp, err := client.SendMessage(ctx, to, msg)
		return resp.ID, resp.Timestamp, err
	}
	s.upload = client.Upload
	s.chatName = func(jid types.JID) string {
		return GetChatName(client, store, jid, jid.String(), nil, "", newSafeLogger("Send"))
	}
	return s
}

// testLockOn: the Test lock is on whenever config/test_lock exists. It is
// checked on every send, so no restart is needed. If the check itself fails
// for any reason other than "file not found" the lock is treated as ON.
func testLockOn() bool {
	_, err := os.Stat(filepath.Join(configDir, testLockFile))
	return err == nil || !os.IsNotExist(err)
}

// parseRecipient accepts a JID or a plain phone number (digits, with or
// without +, spaces or dashes).
func parseRecipient(recipient string) (types.JID, error) {
	recipient = strings.TrimSpace(recipient)
	if strings.Contains(recipient, "@") {
		jid, err := types.ParseJID(strings.TrimPrefix(recipient, "+"))
		if err != nil || jid.User == "" {
			return types.JID{}, fmt.Errorf("invalid JID")
		}
		jid = jid.ToNonAD()
		switch jid.Server {
		case types.DefaultUserServer, types.HiddenUserServer:
			for _, r := range jid.User {
				if r < '0' || r > '9' {
					return types.JID{}, fmt.Errorf("invalid JID")
				}
			}
		case types.GroupServer:
			for _, r := range jid.User {
				if (r < '0' || r > '9') && r != '-' {
					return types.JID{}, fmt.Errorf("invalid JID")
				}
			}
		default:
			return types.JID{}, fmt.Errorf("unsupported JID server")
		}
		return jid, nil
	}
	var digits strings.Builder
	for _, r := range recipient {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		} else if r != '+' && r != ' ' && r != '-' && r != '(' && r != ')' {
			return types.JID{}, fmt.Errorf("invalid phone number")
		}
	}
	if digits.Len() < 5 {
		return types.JID{}, fmt.Errorf("invalid phone number")
	}
	return types.JID{User: digits.String(), Server: types.DefaultUserServer}, nil
}

func (s *Sender) isOwnChat(jid types.JID) bool {
	if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
		return false
	}
	for _, u := range s.ownUsers() {
		if u != "" && u == jid.User {
			return true
		}
	}
	return false
}

func contentHash(text string, media []byte) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(text)))
	h.Write([]byte{0})
	h.Write(media)
	return hex.EncodeToString(h.Sum(nil))
}

// mimeFor maps a file extension to a WhatsApp media type, MIME type and the
// short kind name stored in the database.
func mimeFor(ext string) (whatsmeow.MediaType, string, string) {
	switch ext {
	case "jpg", "jpeg":
		return whatsmeow.MediaImage, "image/jpeg", "image"
	case "png":
		return whatsmeow.MediaImage, "image/png", "image"
	case "gif":
		return whatsmeow.MediaImage, "image/gif", "image"
	case "webp":
		return whatsmeow.MediaImage, "image/webp", "image"
	case "ogg":
		return whatsmeow.MediaAudio, "audio/ogg; codecs=opus", "audio"
	case "mp4":
		return whatsmeow.MediaVideo, "video/mp4", "video"
	case "avi":
		return whatsmeow.MediaVideo, "video/avi", "video"
	case "mov":
		return whatsmeow.MediaVideo, "video/quicktime", "video"
	}
	return whatsmeow.MediaDocument, "application/octet-stream", "document"
}

// Send is the only way anything leaves this bridge. Order of checks:
//  1. recipient is valid
//  2. Test lock (only own chat allowed while config/test_lock exists)
//  3. Repeat lock (same content to same chat within 24h, unless deliberate)
//  4. connected to WhatsApp
//  5. send, then write the Send record
func (s *Sender) Send(recipient, message, mediaPath string, deliberate bool) SendResult {
	to, err := parseRecipient(recipient)
	if err != nil {
		return refuse(http.StatusBadRequest, err.Error())
	}
	chatJID := to.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if testLockOn() && !s.isOwnChat(to) {
		appLog("send REFUSED reason=test_lock chat=%s", chatJID)
		return refuse(http.StatusForbidden, "Test lock is ON (config/test_lock exists): sending is only allowed to your own chat. Remove that file to send to others.")
	}

	var mediaData []byte
	if mediaPath != "" {
		mediaData, err = os.ReadFile(mediaPath)
		if err != nil {
			return refuse(http.StatusBadRequest, "Could not read the media file")
		}
	}
	hash := contentHash(message, mediaData)
	if !deliberate {
		dup, err := s.store.RecentlySent(chatJID, hash, s.now().Add(-repeatLockHours*time.Hour))
		if err != nil {
			// Fail closed: if we cannot tell, do not risk a double send.
			appLog("send REFUSED reason=repeat_check_failed chat=%s", chatJID)
			return refuse(http.StatusInternalServerError, "Could not check the repeat lock; not sending")
		}
		if dup {
			appLog("send REFUSED reason=repeat_lock chat=%s", chatJID)
			return refuse(http.StatusConflict, "Repeat lock: this exact content was already sent to this chat in the last 24 hours. If you really mean to send it again, repeat the call with deliberate_resend=true.")
		}
	}

	if !s.connected() {
		return refuse(http.StatusServiceUnavailable, "Not connected to WhatsApp")
	}

	msg := &waProto.Message{}
	kind := "text"
	fileName := ""
	var up whatsmeow.UploadResponse

	if mediaPath != "" {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(mediaPath), "."))
		mediaType, mimeType, k := mimeFor(ext)
		kind = k
		fileName = filepath.Base(mediaPath)

		up, err = s.upload(context.Background(), mediaData, mediaType)
		if err != nil {
			appLog("send FAILED stage=upload chat=%s err=%v", chatJID, err)
			return refuse(http.StatusInternalServerError, "Error uploading media")
		}
		switch mediaType {
		case whatsmeow.MediaImage:
			msg.ImageMessage = &waProto.ImageMessage{
				Caption: proto.String(message), Mimetype: proto.String(mimeType),
				URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
				FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
			}
		case whatsmeow.MediaAudio:
			seconds, waveform, err := analyzeOggOpus(mediaData)
			if err != nil {
				return refuse(http.StatusBadRequest, "Failed to analyze Ogg Opus file")
			}
			msg.AudioMessage = &waProto.AudioMessage{
				Mimetype: proto.String(mimeType),
				URL:      &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
				FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
				Seconds: proto.Uint32(seconds), PTT: proto.Bool(true), Waveform: waveform,
			}
		case whatsmeow.MediaVideo:
			msg.VideoMessage = &waProto.VideoMessage{
				Caption: proto.String(message), Mimetype: proto.String(mimeType),
				URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
				FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
			}
		case whatsmeow.MediaDocument:
			msg.DocumentMessage = &waProto.DocumentMessage{
				Title: proto.String(fileName), FileName: proto.String(fileName),
				Caption: proto.String(message), Mimetype: proto.String(mimeType),
				URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
				FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
			}
		}
	} else {
		msg.Conversation = proto.String(message)
	}

	id, ts, err := s.transmit(context.Background(), to, msg)
	if err != nil {
		appLog("send FAILED stage=send chat=%s err=%v", chatJID, err)
		return refuse(http.StatusInternalServerError, "Error sending message")
	}
	if ts.IsZero() {
		ts = s.now()
	}

	own := ""
	if users := s.ownUsers(); len(users) > 0 {
		own = users[0]
	}
	rec := SentRecord{
		MessageID: id, ChatJID: chatJID, Kind: kind, Text: message, FileName: fileName,
		ContentHash: hash, SentAt: ts, Deliberate: deliberate,
	}
	if err := s.store.RecordSent(rec, s.chatName(to), own, up.URL, up.MediaKey, up.FileSHA256, up.FileEncSHA256, up.FileLength); err != nil {
		// The message was sent; only the record failed. Say so loudly.
		appLog("send OK but RECORD FAILED chat=%s id=%s err=%v", chatJID, id, err)
		return SendResult{Code: http.StatusOK, Success: true, MessageID: id, Message: "Message sent, but saving the send record failed (check the log)"}
	}
	appLog("send OK chat=%s type=%s id=%s deliberate=%t", chatJID, kind, id, deliberate)
	return SendResult{Code: http.StatusOK, Success: true, MessageID: id, Message: "Message sent"}
}
