package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

	// test fn calling
	testCtrl := controller.NewTestController(aiClient)

	// Initialize the router with all controllers
	r := router.NewRouter(ctrl, homeCtrl, employeeAPI, testCtrl)

	// Set up the routes
	r.SetupRoutes()

	// Create a new server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: nil, // Use default ServeMux
	}

	// Channel to listen for errors coming from the listener.
	serverErrors := make(chan error, 1)

	// Start the server
	go func() {
		fmt.Println("WebSocket AI server and Employee API starting on :8080")
		serverErrors <- srv.ListenAndServe()
	}()

	// Channel to listen for an interrupt or terminate signal from the OS.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Blocking main and waiting for shutdown.
	select {
	case err := <-serverErrors:
		log.Fatalf("Error starting server: %v", err)

	case <-shutdown:
		fmt.Println("Starting shutdown...")

		// Give outstanding requests a deadline for completion.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Asking listener to shut down and shed load.
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Graceful shutdown did not complete in 5s: %v", err)
			if err := srv.Close(); err != nil {
				log.Fatalf("Error killing server: %v", err)
			}
		}
	}

	fmt.Println("Server gracefully stopped")
}