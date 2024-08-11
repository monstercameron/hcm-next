package controller

import (
	"encoding/json"
	"fmt"
	"hcmnext/ai"
	"log"
	"net/http"
)

type TestController struct {
	aiClient *ai.Client
}

func NewTestController(aiClient *ai.Client) *TestController {
	return &TestController{
		aiClient: aiClient,
	}
}

func (c *TestController) HandleSearchSWAPICharacter(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleSearchSWAPICharacter called with method: %s", r.Method)

	// Extract the character name from the query parameters
	characterName := r.URL.Query().Get("name")
	log.Printf("Searching for character: %s", characterName)

	if characterName == "" {
		http.Error(w, "Missing 'name' query parameter", http.StatusBadRequest)
		return
	}

	// Call the SearchSWAPICharacter function
	result, err := c.aiClient.SearchSWAPICharacter(characterName)
	if err != nil {
		log.Printf("Error searching for character: %v", err)
		http.Error(w, fmt.Sprintf("Error searching for character: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a response structure
	response := struct {
		CharacterName string `json:"characterName"`
		SearchResult  string `json:"searchResult"`
	}{
		CharacterName: characterName,
		SearchResult:  result,
	}

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to search request for: %s", characterName)
}