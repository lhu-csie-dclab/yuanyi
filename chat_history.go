// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Server-side chat history persistence. Previously the Chat page (web-ui) kept every session
// in the browser's localStorage only -- switching browsers or devices lost all history. This
// stores each session as a plain-text Markdown file on the machine running the client, one
// file per conversation, so it survives across devices and stays human-readable/editable
// (file-first, in the spirit of keeping state as inspectable text rather than an opaque
// binary/DB blob).

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const chatSessionsDir = "chat_sessions"

// ChatContentPart is one element of a multimodal message's content array (OpenAI vision
// format: [{"type":"text",...},{"type":"image_url",...}]). A text-only message uses a plain
// JSON string instead -- see ChatMessage.Content, which mirrors the frontend's dual shape
// (useChatState.js's send()) via json.RawMessage since Go has no native sum type for this.
type ChatContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type ChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ChatSession is the full save/load payload for one conversation.
type ChatSession struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

// ChatSessionSummary is /api/chat/sessions' list-view shape -- no message bodies (and
// therefore no embedded images), so listing many sessions stays cheap; full content is only
// fetched per-session on demand (GET /api/chat/sessions?id=...).
type ChatSessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at"`
}

// validSessionID gates every filesystem path built from a client-supplied id -- without this,
// an id like "../../../etc/passwd" would let chatSessionPath escape chatSessionsDir entirely.
var validSessionID = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)

// chatImageRefRe is the only markdown image-reference shape resolveChatImageURL will ever
// read off disk -- exactly what writeChatSessionMarkdown itself generates. This is a security
// boundary, not a formatting nicety: a chat message's plain-text content is written into the
// Markdown body verbatim, so a user typing the literal text "![image](../../../../etc/passwd)"
// as a CHAT MESSAGE (not an actual attached image) would, without this check, be reinterpreted
// as a real image reference on the next read and have that arbitrary path read off disk and
// base64-embedded back into the API response. Anything not matching this exact "<id>/img-N.ext"
// shape is left as plain text instead of touched as a filesystem path.
var chatImageRefRe = regexp.MustCompile(`^[0-9A-Za-z_-]+/img-\d+\.[A-Za-z0-9]+$`)

var chatFrontMatterRe = regexp.MustCompile(`^<!--\s*yuanyi-chat\s+id=(\S+)\s+model=(\S*)\s*-->`)
var chatMsgMarkerRe = regexp.MustCompile(`(?m)^<!--\s*msg\s+role=(user|assistant)\s*-->\s*$`)
var chatImageLineRe = regexp.MustCompile(`^!\[image\]\(([^)]+)\)$`)
var dataURLRe = regexp.MustCompile(`^data:image/([a-zA-Z0-9.+-]+);base64,(.+)$`)

func chatSessionPath(id string) (string, error) {
	if !validSessionID.MatchString(id) {
		return "", fmt.Errorf("invalid session id")
	}
	return filepath.Join(chatSessionsDir, id+".md"), nil
}

func chatSessionImageDir(id string) string {
	return filepath.Join(chatSessionsDir, id)
}

// decodeMessageContent splits a message's Content (plain string XOR content-part array) into
// its text and any attached image data URLs.
func decodeMessageContent(raw json.RawMessage) (text string, images []string) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", nil
	}
	for _, p := range parts {
		if p.Type == "text" {
			text += p.Text
		} else if p.Type == "image_url" && p.ImageURL != nil {
			images = append(images, p.ImageURL.URL)
		}
	}
	return text, images
}

// writeChatSessionMarkdown persists a full session, extracting any inline base64 image data
// URLs into sibling files (chat_sessions/<id>/img-N.ext) so the .md file itself stays lean and
// genuinely readable in a plain text editor, rather than bloated with megabytes of base64.
func writeChatSessionMarkdown(sess ChatSession) error {
	path, err := chatSessionPath(sess.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(chatSessionsDir, 0755); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<!-- yuanyi-chat id=%s model=%s -->\n", sess.ID, sess.Model)
	fmt.Fprintf(&b, "# %s\n\n", sess.Title)

	imgN := 0
	for _, msg := range sess.Messages {
		roleLabel := "🤖 Assistant"
		if msg.Role == "user" {
			roleLabel = "👤 User"
		}
		fmt.Fprintf(&b, "<!-- msg role=%s -->\n### %s\n\n", msg.Role, roleLabel)

		text, images := decodeMessageContent(msg.Content)
		for _, imgURL := range images {
			m := dataURLRe.FindStringSubmatch(imgURL)
			if m == nil {
				// Not a data URL (e.g. an http(s) image link) -- reference it directly rather
				// than trying to fetch and re-host it.
				fmt.Fprintf(&b, "![image](%s)\n\n", imgURL)
				continue
			}
			ext := m[1]
			raw, err := base64.StdEncoding.DecodeString(m[2])
			if err != nil {
				continue
			}
			imgN++
			imgDir := chatSessionImageDir(sess.ID)
			if err := os.MkdirAll(imgDir, 0755); err != nil {
				continue
			}
			imgFile := fmt.Sprintf("img-%d.%s", imgN, ext)
			if err := os.WriteFile(filepath.Join(imgDir, imgFile), raw, 0644); err != nil {
				continue
			}
			fmt.Fprintf(&b, "![image](%s/%s)\n\n", sess.ID, imgFile)
		}
		if text != "" {
			fmt.Fprintf(&b, "%s\n\n", text)
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// resolveChatImageURL turns a markdown image reference back into something the frontend can
// render directly: passed through unchanged for http(s)/data URLs, or read off disk and
// re-encoded as a data URL for a reference matching chatImageRefRe. Anything else returns ""
// (see chatImageRefRe's comment for why this must not just filepath.Join and read).
func resolveChatImageURL(ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "data:") {
		return ref
	}
	if !chatImageRefRe.MatchString(ref) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(chatSessionsDir, ref))
	if err != nil {
		return ""
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(ref), "."))
	mime := "image/" + ext
	if ext == "jpg" {
		mime = "image/jpeg"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
}

// parseChatMessageSegment turns the raw text between one <!-- msg --> marker and the next
// back into a ChatMessage, resolving any recognized image references along the way.
func parseChatMessageSegment(role, segment string) ChatMessage {
	var textLines []string
	var images []string
	skippedHeader := false
	for _, line := range strings.Split(segment, "\n") {
		trimmed := strings.TrimSpace(line)
		if !skippedHeader {
			if strings.HasPrefix(trimmed, "###") {
				skippedHeader = true
				continue
			}
			if trimmed == "" {
				continue
			}
		}
		if m := chatImageLineRe.FindStringSubmatch(trimmed); m != nil {
			if url := resolveChatImageURL(m[1]); url != "" {
				images = append(images, url)
				continue
			}
			// Looked like an image line but didn't resolve to a trusted reference -- keep it
			// as plain text rather than silently dropping the line.
		}
		textLines = append(textLines, line)
	}
	text := strings.TrimSpace(strings.Join(textLines, "\n"))

	var contentRaw json.RawMessage
	if len(images) == 0 {
		contentRaw, _ = json.Marshal(text)
	} else {
		parts := []ChatContentPart{}
		if text != "" {
			parts = append(parts, ChatContentPart{Type: "text", Text: text})
		}
		for _, url := range images {
			u := url
			parts = append(parts, ChatContentPart{Type: "image_url", ImageURL: &struct {
				URL string `json:"url"`
			}{URL: u}})
		}
		contentRaw, _ = json.Marshal(parts)
	}
	return ChatMessage{Role: role, Content: contentRaw}
}

// readChatSessionMarkdown parses a session's full content, including resolving every attached
// image back to a data URL -- expensive relative to readChatSessionHeader, so only used for a
// single selected session, never for the list view.
func readChatSessionMarkdown(id string) (*ChatSession, error) {
	path, err := chatSessionPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	sess := &ChatSession{ID: id}
	firstLineEnd := strings.IndexByte(content, '\n')
	firstLine := content
	rest := ""
	if firstLineEnd >= 0 {
		firstLine = content[:firstLineEnd]
		rest = content[firstLineEnd+1:]
	}
	if m := chatFrontMatterRe.FindStringSubmatch(firstLine); m != nil {
		sess.Model = m[2]
	}

	for _, line := range strings.Split(rest, "\n") {
		l := strings.TrimRight(line, "\r")
		if strings.HasPrefix(l, "# ") {
			sess.Title = strings.TrimSpace(strings.TrimPrefix(l, "# "))
			break
		}
		if strings.TrimSpace(l) == "#" {
			break // empty title, written as "# " with nothing after it
		}
	}

	markers := chatMsgMarkerRe.FindAllStringSubmatchIndex(rest, -1)
	for i, m := range markers {
		role := rest[m[2]:m[3]]
		segStart := m[1]
		segEnd := len(rest)
		if i+1 < len(markers) {
			segEnd = markers[i+1][0]
		}
		sess.Messages = append(sess.Messages, parseChatMessageSegment(role, rest[segStart:segEnd]))
	}

	return sess, nil
}

// readChatSessionHeader extracts just the title/model without processing message bodies or
// resolving any images, keeping /api/chat/sessions' list view cheap even with many sessions.
func readChatSessionHeader(id string) (title, model string, err error) {
	path, perr := chatSessionPath(id)
	if perr != nil {
		return "", "", perr
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if m := chatFrontMatterRe.FindStringSubmatch(line); m != nil {
			model = m[2]
			continue
		}
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
		if strings.TrimSpace(line) == "#" {
			break
		}
	}
	return title, model, nil
}

func listChatSessions() ([]ChatSessionSummary, error) {
	entries, err := os.ReadDir(chatSessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChatSessionSummary{}, nil
		}
		return nil, err
	}

	out := []ChatSessionSummary{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if !validSessionID.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		title, model, err := readChatSessionHeader(id)
		if err != nil {
			continue
		}
		out = append(out, ChatSessionSummary{ID: id, Title: title, Model: model, UpdatedAt: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func deleteChatSession(id string) error {
	path, err := chatSessionPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	os.RemoveAll(chatSessionImageDir(id))
	return nil
}

// RegisterChatRoutes mounts the chat-history API under /api/chat/sessions. Registered
// unconditionally (unlike RegisterHubRoutes) -- chat history isn't a hub-only feature.
func RegisterChatRoutes(mux *http.ServeMux, app *App) {
	mux.HandleFunc("/api/chat/sessions", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")

		switch r.Method {
		case http.MethodGet:
			if id == "" {
				list, err := listChatSessions()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(list)
				return
			}
			sess, err := readChatSessionMarkdown(id)
			if err != nil {
				http.Error(w, "session not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sess)

		case http.MethodPost:
			if id == "" {
				http.Error(w, "missing id", http.StatusBadRequest)
				return
			}
			var sess ChatSession
			if err := json.NewDecoder(r.Body).Decode(&sess); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sess.ID = id
			if err := writeChatSessionMarkdown(sess); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))

		case http.MethodDelete:
			if id == "" {
				http.Error(w, "missing id", http.StatusBadRequest)
				return
			}
			if err := deleteChatSession(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
