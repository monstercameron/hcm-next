package controller

import (
	"fmt"
	"net/http"

	"hcmnext/ai"
	"hcmnext/database"

	"github.com/coder/websocket"
)

type Controller struct {
	aiClient *ai.Client
	db       *database.Database
}

func NewController(aiClient *ai.Client, db *database.Database) *Controller {
	return &Controller{
		aiClient: aiClient,
		db:       db,
	}
}

// HandleWebSocket manages the WebSocket connection
func (c *Controller) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
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
		aiResponse, err := c.aiClient.HandleRequest(string(msg))
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