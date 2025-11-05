package api

import (
	"backend/internal/engine"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	engine   *engine.SyncEngine
	clients  map[*websocket.Conn]bool
	clientMu sync.RWMutex
	upgrader websocket.Upgrader
	events   chan SyncEvent
}

type SyncEvent struct {
	Type      string    `json:"type"`
	FilePath  string    `json:"filePath"`
	Direction string    `json:"direction"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

type StatusResponse struct {
	Status      string `json:"status"`
	LocalFiles  int    `json:"localFiles"`
	RemoteFiles int    `json:"remoteFiles"`
	IsRunning   bool   `json:"isRunning"`
	IsPaused    bool   `json:"isPaused"`
}

type SyncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func NewServer(engine *engine.SyncEngine) *Server {
	server := &Server{
		engine:  engine,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		events: make(chan SyncEvent, 100),
	}
	
	// Register callback to receive sync events from engine
	engine.SetEventCallback(func(eventType, filePath, direction, message string) {
		server.NotifyEvent(SyncEvent{
			Type:      eventType,
			FilePath:  filePath,
			Direction: direction,
			Timestamp: time.Now(),
			Message:   message,
		})
	})
	
	return server
}

func (s *Server) Start(port string) error {
	http.HandleFunc("/api/status", s.handleStatus)
	http.HandleFunc("/api/files", s.handleFiles)
	http.HandleFunc("/api/pause", s.handlePause)
	http.HandleFunc("/api/resume", s.handleResume)
	http.HandleFunc("/api/sync", s.handleManualSync)
	http.HandleFunc("/ws", s.handleWebSocket)
	
	// Enable CORS
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})

	go s.broadcastEvents()

	log.Printf("API server starting on port %s\n", port)
	return http.ListenAndServe(":"+port, nil)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	status := StatusResponse{
		Status:      "running",
		LocalFiles:  s.engine.GetLocalFileCount(),
		RemoteFiles: s.engine.GetRemoteFileCount(),
		IsRunning:   true,
		IsPaused:    s.engine.IsPaused(),
	}

	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	files := s.engine.GetFileList()
	json.NewEncoder(w).Encode(files)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()

	s.clientMu.Lock()
	s.clients[conn] = true
	s.clientMu.Unlock()

	log.Printf("Client connected via WebSocket. Total clients: %d\n", len(s.clients))

	// Keep connection alive and read messages (ping/pong)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			s.clientMu.Lock()
			delete(s.clients, conn)
			s.clientMu.Unlock()
			log.Printf("Client disconnected. Total clients: %d\n", len(s.clients))
			break
		}
	}
}

func (s *Server) broadcastEvents() {
	for event := range s.events {
		s.clientMu.RLock()
		for client := range s.clients {
			err := client.WriteJSON(event)
			if err != nil {
				log.Printf("Error sending event to client: %v\n", err)
				client.Close()
				s.clientMu.Lock()
				delete(s.clients, client)
				s.clientMu.Unlock()
			}
		}
		s.clientMu.RUnlock()
	}
}

func (s *Server) NotifyEvent(event SyncEvent) {
	select {
	case s.events <- event:
	default:
		log.Println("Event channel full, dropping event")
	}
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.engine.Pause()
	
	response := SyncResponse{
		Success: true,
		Message: "Sync engine paused",
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.engine.Resume()
	
	response := SyncResponse{
		Success: true,
		Message: "Sync engine resumed",
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleManualSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := s.engine.ManualSync()
	if err != nil {
		response := SyncResponse{
			Success: false,
			Message: err.Error(),
		}
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}
	
	response := SyncResponse{
		Success: true,
		Message: "Manual sync completed successfully",
	}
	json.NewEncoder(w).Encode(response)
	
	// Notify connected clients
	s.NotifyEvent(SyncEvent{
		Type:      "sync",
		FilePath:  "manual",
		Direction: "both",
		Timestamp: time.Now(),
		Message:   "Manual sync triggered",
	})
}
