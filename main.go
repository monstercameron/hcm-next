package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"hcmnext/ai"
	"hcmnext/database"

	"github.com/coder/websocket"
	"github.com/joho/godotenv"
)

// Global variables
var (
	aiClient *ai.Client
	db       *database.Database
)

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
}

// handleWebSocket manages the WebSocket connection
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:5500", "127.0.0.1:5500"},
	})
	if err != nil {
		fmt.Printf("WebSocket accept error: %v\n", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing connection")

	fmt.Println("WebSocket connection established")

	for {
		// Read message from client
		_, msg, err := conn.Read(r.Context())
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
			break
		}

		fmt.Printf("Received message from client: %s\n", msg)

		// Send the message to the AI and get the response
		aiResponse, err := aiClient.HandleRequest(string(msg))
		if err != nil {
			fmt.Printf("AI request error: %v\n", err)
			break
		}

		// Send the AI's response back to the client
		err = conn.Write(r.Context(), websocket.MessageText, []byte(aiResponse))
		if err != nil {
			fmt.Printf("Write error: %v\n", err)
			break
		}

		fmt.Println("Response sent to client")
	}

	fmt.Println("WebSocket connection closed")
}

func main() {
	// Initialize AI client
	var err error
	aiClient, err = ai.NewClient()
	if err != nil {
		log.Fatalf("Failed to initialize AI client: %v", err)
	}
	fmt.Println("AI client initialized")

	// Database initialization
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI not set in environment variables")
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		log.Fatal("DB_NAME not set in environment variables")
	}

	db, err = database.NewDatabase(mongoURI, dbName)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("Connected to MongoDB")

	// Set up the WebSocket handler
	http.HandleFunc("/", handleWebSocket)

	// Start the server
	fmt.Println("WebSocket AI server starting on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}