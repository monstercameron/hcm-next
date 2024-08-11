package controller

import (
	"encoding/json"
	"fmt"
	openai "github.com/sashabaranov/go-openai"
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

// ExecutionPlan struct to hold the unmarshaled data
type ExecutionPlan struct {
	Tools   []string `json:"tools"`
	Context string   `json:"context"`
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

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(executionPlan); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to execution plan request for: %s", prompt)
}

type ToolUseResponse struct {
	Tool    bool   `json:"tool"`
	Context string `json:"context"`
}

func (c *TestController) HandleToolUse(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleGenerateExecutionPlan called with method: %s", r.Method)

	// extract the body json and marshal it into a []openai.ChatCompletionMessage
	var messages []openai.ChatCompletionMessage
	if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, fmt.Sprintf("Error decoding request body: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("should use tool for: %v", messages)

	// Call the GenerateExecutionPlan function
	result, err := c.aiClient.ShouldUseTool(messages)
	if err != nil {
		log.Printf("Error should use tool: %v", err)
		http.Error(w, fmt.Sprintf("Error should use tool: %v", err), http.StatusInternalServerError)
		return
	}

	// Define a variable to hold the unmarshaled data
	var toolUseResponse ToolUseResponse

	// Unmarshal the JSON string into the struct
	if err := json.Unmarshal([]byte(result), &toolUseResponse); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		http.Error(w, fmt.Sprintf("Error unmarshaling JSON: %v", err), http.StatusInternalServerError)
		return
	}

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(toolUseResponse); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to execution plan request for: %v", messages)
}

func (c *TestController) HandleGenerateMath(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleGenerateExecutionPlan called with method: %s", r.Method)

	// Extract the prompt from the query parameters
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "Missing 'prompt' query parameter", http.StatusBadRequest)
		return
	}

	log.Printf("Generating Math via Python for: %s", prompt)

	// Call the GenerateExecutionPlan function
	result, err := c.aiClient.GenerateMath(prompt)
	if err != nil {
		log.Printf("Error Generating Math via Python: %v", err)
		http.Error(w, fmt.Sprintf("Error Generating Math via Python: %v", err), http.StatusInternalServerError)
		return
	}

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to execution plan request for: %s", prompt)
}

func (c *TestController) HandleGenerateDisplayHtml(w http.ResponseWriter, r *http.Request) {
	log.Printf("HandleGenerateDisplayHtml called with method: %s", r.Method)

	// Extract the prompt from the query parameters
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "Missing 'prompt' query parameter", http.StatusBadRequest)
		return
	}

	log.Printf("Generating HTML for: %s", prompt)

	// Call the GenerateExecutionPlan function
	result, err := c.aiClient.GenerateDisplayHtml(prompt)
	if err != nil {
		log.Printf("Error HTML: %v", err)
		http.Error(w, fmt.Sprintf("Error HTML: %v", err), http.StatusInternalServerError)
		return
	}

	// Set the content type to JSON
	w.Header().Set("Content-Type", "application/json")

	// Encode and send the response
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, fmt.Sprintf("Error encoding response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully responded to execution plan request for: %s", prompt)
}