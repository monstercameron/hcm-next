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

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	type Character struct {
		URL           string `json:"URL"`
		CharacterName string `json:"characterName"`
	}

	// Define a variable to hold the unmarshaled data
	var resultStruct Character

	// Unmarshal the JSON string into the struct
	if err := json.Unmarshal([]byte(result), &resultStruct); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		http.Error(w, fmt.Sprintf("Error unmarshaling JSON: %v", err), http.StatusInternalServerError)
		return
	}

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(resultStruct); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to search request for: %s", characterName)
}

// ExecutionPlan struct to hold the unmarshaled data
type ExecutionPlan struct {
	Tools   string `json:"tools"`
	Context string `json:"context"`
}

func (c *TestController) HandleGenerateExecutionPlan(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleGenerateExecutionPlan called with method: %s", r.Method)

	// Extract the prompt from the query parameters
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "Missing 'prompt' query parameter", http.StatusBadRequest)
		return
	}

	log.Printf("Generating Execution Plan for: %s", prompt)

	// Call the GenerateExecutionPlan function
	result, err := c.aiClient.GenerateExecutionPlan(prompt)
	if err != nil {
		log.Printf("Error Generating Execution Plan: %v", err)
		http.Error(w, fmt.Sprintf("Error Generating Execution Plan: %v", err), http.StatusInternalServerError)
		return
	}

	// Define a variable to hold the unmarshaled data
	var executionPlan ExecutionPlan

	// Unmarshal the JSON string into the struct
	if err := json.Unmarshal([]byte(result), &executionPlan); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		http.Error(w, fmt.Sprintf("Error unmarshaling JSON: %v", err), http.StatusInternalServerError)
		return
	}

	// Unmarshal the Tools field into a slice of strings
	var tools []string
	if err := json.Unmarshal([]byte(executionPlan.Tools), &tools); err != nil {
		log.Printf("Error unmarshaling tools JSON string: %v", err)
		http.Error(w, fmt.Sprintf("Error unmarshaling tools JSON string: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a new struct with the unmarshaled tools
	response := struct {
		Tools   []string `json:"tools"`
		Context string   `json:"context"`
	}{
		Tools:   tools,
		Context: executionPlan.Context,
	}

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to execution plan request for: %s", prompt)
}
