package ai

import (
	"context"
	"fmt"
	"os"

	openai "github.com/sashabaranov/go-openai"
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
			Model: "gpt-4-0125-preview",
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