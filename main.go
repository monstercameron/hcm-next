package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/coder/websocket"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

// Global OpenAI client
var aiClient *openai.Client

func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize OpenAI client with API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OpenAI API key not set")
	}
	aiClient = openai.NewClient(apiKey)
	fmt.Println("OpenAI client initialized")
}

// handleAIRequest sends a message to OpenAI and returns the response
func handleAIRequest(message string) (string, error) {
	fmt.Printf("Sending message to OpenAI: %s\n", message)

	resp, err := aiClient.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: "gpt-4o-mini",
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    "user",
					Content: message,
				},
			},
		},
	)
	if err != nil {
		fmt.Printf("Error from OpenAI: %v\n", err)
		return "", err
	}

	aiResponse := resp.Choices[0].Message.Content
	fmt.Printf("Received response from OpenAI: %s\n", aiResponse)
	return aiResponse, nil
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
		aiResponse, err := handleAIRequest(string(msg))
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
	// Set up the WebSocket handler
	http.HandleFunc("/", handleWebSocket)

	// Start the server
	fmt.Println("WebSocket AI server starting on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
