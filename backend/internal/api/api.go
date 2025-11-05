package api

import (
	"backend/internal/engine"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Server struct {
	engine   *engine.SyncEngine
	clients  map[*websocket.Conn]bool
	clientMu sync.RWMutex
	upgrader websocket.Upgrader
	events   chan SyncEvent
	router   *gin.Engine
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
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	server := &Server{
		engine:  engine,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		events: make(chan SyncEvent, 100),
		router: router,
	}

	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}
		c.Next()
	})

	apiGroup := router.Group("/api")
	apiGroup.GET("/status", server.handleStatus)
	apiGroup.GET("/files", server.handleFiles)
	apiGroup.POST("/pause", server.handlePause)
	apiGroup.POST("/resume", server.handleResume)
	apiGroup.POST("/sync", server.handleManualSync)

	router.GET("/ws", server.handleWebSocket)

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
	go s.broadcastEvents()

	log.Printf("API server starting on port %s\n", port)
	return s.router.Run(":" + port)
}

func (s *Server) handleStatus(c *gin.Context) {
	status := StatusResponse{
		Status:      "running",
		LocalFiles:  s.engine.GetLocalFileCount(),
		RemoteFiles: s.engine.GetRemoteFileCount(),
		IsRunning:   true,
		IsPaused:    s.engine.IsPaused(),
	}

	c.JSON(http.StatusOK, status)
}

func (s *Server) handleFiles(c *gin.Context) {
	files := s.engine.GetFileList()
	c.JSON(http.StatusOK, files)
}

func (s *Server) handleWebSocket(c *gin.Context) {
	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
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

func (s *Server) handlePause(c *gin.Context) {
	s.engine.Pause()

	response := SyncResponse{
		Success: true,
		Message: "Sync engine paused",
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) handleResume(c *gin.Context) {
	s.engine.Resume()

	response := SyncResponse{
		Success: true,
		Message: "Sync engine resumed",
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) handleManualSync(c *gin.Context) {
	err := s.engine.ManualSync()
	if err != nil {
		response := SyncResponse{
			Success: false,
			Message: err.Error(),
		}
		c.JSON(http.StatusBadRequest, response)
		return
	}

	response := SyncResponse{
		Success: true,
		Message: "Manual sync completed successfully",
	}
	c.JSON(http.StatusOK, response)

	// Notify connected clients
	s.NotifyEvent(SyncEvent{
		Type:      "sync",
		FilePath:  "manual",
		Direction: "both",
		Timestamp: time.Now(),
		Message:   "Manual sync triggered",
	})
}
