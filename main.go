package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"hcmnext/ai"
	"hcmnext/database"
	"hcmnext/controller"
	"hcmnext/router"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
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

	// Check for collections and count their contents
	collections := []string{"Employee", "Job"}
	for _, collName := range collections {
		count, err := db.CountDocuments(collName, bson.M{})
		if err != nil {
			log.Printf("Error counting documents in %s collection: %v", collName, err)
		} else {
			fmt.Printf("Collection %s exists and contains %d documents\n", collName, count)
		}
	}

	// Initialize the controller
	ctrl := controller.NewController(aiClient, db)

	// Initialize the home controller
	staticDir := filepath.Join(".", "static")
	homeCtrl := controller.NewHomeController(staticDir)

	// Initialize the Employee API
	employeeAPI := controller.NewAPI(db)

	// Initialize the router with all controllers
	r := router.NewRouter(ctrl, homeCtrl, employeeAPI)

	// Set up the routes
	r.SetupRoutes()

	// Start the server
	fmt.Println("WebSocket AI server and Employee API starting on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}