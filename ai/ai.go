package ai

import (
	"context"
	"fmt"
	"os"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// Client represents the AI client
type Client struct {
	aiClient *openai.Client
}

// NewClient creates a new AI client
func NewClient() (*Client, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not set")
	}
	return &Client{
		aiClient: openai.NewClient(apiKey),
	}, nil
}

// HandleRequest sends a message to OpenAI and returns the response
func (c *Client) HandleRequest(message string) (string, error) {
	fmt.Printf("Sending message to OpenAI: %s\n", message)

	resp, err := c.aiClient.CreateChatCompletion(
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

func (c *Client) SearchSWAPICharacter(characterName string) (string, error) {
	ctx := context.Background()

	// Define the function and its parameters
	params := jsonschema.Definition{
		Type: jsonschema.Object,
		Properties: map[string]jsonschema.Definition{
			"characterName": {
				Type:        jsonschema.String,
				Description: "The full name of the character to retrieve information for in the SWAPI.",
			},
			"URL": {
				Type:        jsonschema.String,
				Description: "The URL to call for retrieving the character information from SWAPI.",
			},
		},
		Required: []string{"characterName", "URL"},
	}

	functionDefinition := openai.FunctionDefinition{
		Name:        "generate_swapi_retrieval_url",
		Description: "Generate a URL to retrieve the character information from SWAPI.",
		Parameters:  params,
	}

	tool := openai.Tool{
		Type:     openai.ToolTypeFunction,
		Function: &functionDefinition,
	}

	// Prepare the initial user message
	dialogue := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleUser,
			Content: fmt.Sprintf("generate SWAPI URL '%s'", characterName),
		},
	}

	fmt.Printf("Asking OpenAI '%v' and providing it a '%v()' function...\n",
		dialogue[0].Content, functionDefinition.Name)

	// Send the request to OpenAI
	resp, err := c.aiClient.CreateChatCompletion(ctx,
		openai.ChatCompletionRequest{
			Model:    "gpt-4o-mini",
			Messages: dialogue,
			Tools:    []openai.Tool{tool},
		},
	)
	if err != nil || len(resp.Choices) != 1 {
		fmt.Printf("Completion error: err:%v len(choices):%v\n", err, len(resp.Choices))
		return "", err
	}

	// Process the response and function call
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		fmt.Printf("Completion error: len(toolcalls): %v\n", len(msg.ToolCalls))
		return "", fmt.Errorf("unexpected number of tool calls")
	}

	// Directly use the Arguments as a string
	searchURL := msg.ToolCalls[0].Function.Arguments
	fmt.Printf("OpenAI generated the URL: %s\n", searchURL)

	// Simulate calling the SWAPI search function and responding to OpenAI
	dialogue = append(dialogue, msg)
	dialogue = append(dialogue, openai.ChatCompletionMessage{
		Role:       openai.ChatMessageRoleTool,
		Content:    searchURL,
		Name:       msg.ToolCalls[0].Function.Name,
		ToolCallID: msg.ToolCalls[0].ID,
	})

	fmt.Printf("Sending OpenAI our '%v()' function's response and requesting the reply to the original question...\n",
		functionDefinition.Name)

	// Get the final response from OpenAI
	resp, err = c.aiClient.CreateChatCompletion(ctx,
		openai.ChatCompletionRequest{
			Model:    openai.GPT4TurboPreview,
			Messages: dialogue,
			Tools:    []openai.Tool{tool},
		},
	)
	if err != nil || len(resp.Choices) != 1 {
		fmt.Printf("2nd completion error: err:%v len(choices):%v\n", err, len(resp.Choices))
		return "", err
	}

	// Return the final response
	msg = resp.Choices[0].Message
	fmt.Printf("OpenAI answered the original request with: %v\n",
		msg.Content)
	return searchURL, nil
}
