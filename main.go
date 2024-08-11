package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"hcmnext/ai"
	"hcmnext/database"
	"hcmnext/controller"

	"github.com/joho/godotenv"
)

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}
}

func main() {
	// Initialize AI client
	aiClient, err := ai.NewClient()
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

	db, err := database.NewDatabase(mongoURI, dbName)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("Connected to MongoDB")

	// Initialize the controller
	ctrl := controller.NewController(aiClient, db)

	// Set up the WebSocket handler
	http.HandleFunc("/", ctrl.HandleWebSocket)

	// Start the server
	fmt.Println("WebSocket AI server starting on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}