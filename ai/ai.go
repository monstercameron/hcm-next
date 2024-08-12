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

	// test integrating the go fns in the chat
	markup, err := c.GenerateDisplayHtml(message)
	if err != nil {
		fmt.Printf("Error from OpenAI: %v\n", err)
		return "", err
	}

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


	// return aiResponse, nil
	return fmt.Sprintf("```display %s```", markup.Markup), nil
}

func (c *Client) GenerateExecutionPlan(prompt string) (string, error) {
	ctx := context.Background()

	var schema = openai.ChatCompletionResponseFormatJSONSchema{
		Name:        "GenerateExecutionPlan",
		Description: "For a given user prompt, generate an execution plan. This is a tool that returns an array of steps to execute the given task.",
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"tools": {
					Type:        jsonschema.Array,
					Description: `An array of steps as strings to execute the given task. Example: ["generateApi", "callApi", "parseResponse", "generateDisplayHtml", "generateOutput"]`,
					Items: &jsonschema.Definition{Type: jsonschema.String},
				},
				"context": {
					Type:        jsonschema.String,
					Description: "An explanation of why this tool is needed step by step.",
				},
			},
			Required:             []string{"tools", "context"},
			AdditionalProperties: false,
		},
		Strict: true,
	}
	
	// Prepare the initial user message
	dialogue := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintln(`I'll help you generate an execution plan, tools are a list of escaped json strings inside a string, You have access to a comprehensive set of tools designed to perform a wide range of tasks, from generating API calls to producing the final output for display. Each tool has a specific function that contributes to the overall process of executing a task. Here is the list of tools available:

generateApi: Generates an API call based on the provided parameters.
callApi: Executes the API call and retrieves the data.
parseResponse: Parses the response received from the API call into a usable format.
generateDisplayHtml: Generates the HTML structure needed to display the parsed data.
generateOutput: Generates the final output in the chat format, ready for display.
cacheResults: Stores intermediate results to optimize multistage processes.
generateMath: Creates mathematical expressions or calculations.
executeMath: Performs mathematical calculations based on generated expressions.
fetchDatabase: Retrieves data from a database.
storeData: Saves data into a database.
processData: Processes raw data into meaningful information.
filterData: Filters data based on specific criteria.
sortData: Sorts data in ascending or descending order.
transformData: Transforms data into a different format or structure.
generateReport: Generates reports from processed data.
sendEmail: Sends an email with the generated report or other data.
generateGraph: Creates visual graphs from data.
executeScript: Executes a script or a sequence of commands.
logActivity: Logs the activities performed during the execution plan.
Example Tool Usages for Execution Plans
Search for a user in the database:

Tools: [generateApi, callApi, parseResponse, generateDisplayHtml, generateOutput]
Context: "Search for a user by their email address."
Calculate the sum of two numbers (113124 and 9201):

Tools: [generateMath, executeMath, generateDisplayHtml, generateOutput]
Context: "Calculate the sum of two numbers."
Retrieve and sort customer data:

Tools: [fetchDatabase, filterData, sortData, generateReport, generateOutput]
Context: "Retrieve customer data, filter for active users, and sort by registration date."
Generate and send a sales report:

Tools: [fetchDatabase, processData, generateReport, sendEmail, logActivity]
Context: "Generate a sales report for Q2 and send it to the finance department."
Fetch and display product information:

Tools: [generateApi, callApi, parseResponse, generateDisplayHtml, generateOutput]
Context: "Fetch product details by product ID and display them on the website."
Calculate and graph monthly revenue:

Tools: [fetchDatabase, executeMath, generateGraph, generateReport, generateOutput]
Context: "Calculate monthly revenue and generate a graph."
Create a user account and log the activity:

Tools: [generateApi, callApi, parseResponse, storeData, logActivity]
Context: "Create a new user account and log the creation event."
Process and transform sales data:

Tools: [fetchDatabase, processData, transformData, generateReport, generateOutput]
Context: "Process sales data and transform it into a different format."
Generate a list of top-selling products:

Tools: [fetchDatabase, filterData, sortData, generateReport, generateOutput]
Context: "Generate a report of the top-selling products for the last quarter."
Fetch weather data and display it in a dashboard:

Tools: [generateApi, callApi, parseResponse, generateDisplayHtml, generateOutput]
Context: "Fetch current weather data for a specific location and display it on the dashboard."
Complex Multistage Execution Plan Examples
Example 1: Multistage Task - Generate a Large List of Processed Weather Data
Objective: Fetch weather data for multiple locations, process and compile key weather metrics into a list, and output the entire list for display, using cacheResults between stages to manage intermediate data.

Stage 1: Fetch Weather Data for Multiple Locations

Tools: [generateApi, callApi, cacheResults]
Context: "Fetch the weather data for multiple locations (e.g., 100 cities)."
Process: Generate and execute API calls for each location, then cache the raw weather data responses.
Stage 2: Process and Compile Weather Metrics

Tools: [parseResponse, processData, cacheResults]
Context: "Process the cached weather data to extract and compile a list of key metrics (temperature, humidity, wind speed) for each location."
Process: Parse the cached data, extract relevant metrics, and compile them into a large list. Cache the processed list for further use.
Stage 3: Generate and Output the Final List

Tools: [generateDisplayHtml, generateOutput]
Context: "Generate the HTML structure to display the compiled list of weather metrics and output it in the final chat format."
Process: Use the cached list to generate the HTML and output the compiled information for display.
Example 2: Multistage Task - Generate and Output a Large List of Monthly Performance Data
Objective: Retrieve and process performance data for multiple departments, compile the data into a comprehensive list, and output the list for reporting, utilizing cacheResults to manage the intermediate results.

Stage 1: Retrieve Performance Data for Multiple Departments

Tools: [fetchDatabase, cacheResults]
Context: "Fetch the monthly performance data for multiple departments (e.g., 50 departments)."
Process: Retrieve the data for each department and cache the raw performance data for processing.
Stage 2: Process and Compile Performance Data

Tools: [processData, generateReport, cacheResults]
Context: "Process the cached performance data to compile a large list of key metrics (e.g., sales, customer satisfaction) for each department."
Process: Process the cached data to extract key performance metrics, compile them into a large list, and cache the processed list.
Stage 3: Generate and Output the Final List

Tools: [generateDisplayHtml, generateOutput]
Context: "Generate the HTML structure to display the compiled list of performance metrics and output it in the final report format."
Process: Use the cached list to generate the HTML and produce the final output for display or reporting.`),
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: fmt.Sprintf("create an execution plan json based on this prompt: '%s'", prompt),
		},
	}

	// fmt.Printf("Asking OpenAI '%v' and providing it a '%v()' function...\n",
	// 	dialogue[0].Content, functionDefinition.Name)

	// Send the request to OpenAI
	resp, err := c.aiClient.CreateChatCompletion(ctx,
		openai.ChatCompletionRequest{
			Model:    "gpt-4o-mini",
			Messages: dialogue,
			//Tools:    []openai.Tool{tool},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type:       openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &schema,
			},
		},
	)
	if err != nil || len(resp.Choices) != 1 {
		fmt.Printf("Completion error: err:%v len(choices):%v\n", err, len(resp.Choices))
		return "", err
	}

	// Process the response and function call
	msg := resp.Choices[0].Message
	
	// Return the final response
	msg = resp.Choices[0].Message
	fmt.Printf("OpenAI answered the original request with: %v\n",
		msg.Content)
	return msg.Content, nil
}

func (c *Client) ShouldUseTool(consersation []openai.ChatCompletionMessage) (string, error) {
	ctx := context.Background()

	var schema = openai.ChatCompletionResponseFormatJSONSchema{
		Name:        "ShouldUseTool",
		Description: "For a given user prompt, determine whether we should use tools to help the user or if the provided context is sufficient to provide a response.",
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"useTool": {
					Type:        jsonschema.Boolean,
					Description: `returns a boolean value indicating whether the tool should be used or not. example: "useTool": true`,
				},
				"context": {
					Type:        jsonschema.String,
					Description: "An explanation of why this tool is needed step by step.",
				},
			},
			Required:             []string{"useTool", "context"},
			AdditionalProperties: false,
		},
		Strict: true,
	}

	// Prepare the initial user message
	dialogue := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: fmt.Sprintln(`I can help you decide whether if a tool should be used or not based on our conversation. I will return a tool call that will return a boolean value indicating whether the tool should be used or not. Always return json `),
		},
	}

	// Add the conversation to the dialogue
	dialogue = append(dialogue, consersation...)

	// Send the request to OpenAI
	resp, err := c.aiClient.CreateChatCompletion(ctx,
		openai.ChatCompletionRequest{
			Model:    "gpt-4o-mini",
			Messages: dialogue,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type:       openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &schema,
			},
		},
	)

	if err != nil || len(resp.Choices) != 1 {
		fmt.Printf("Completion error: err:%v len(choices):%v\n", err, len(resp.Choices))
		return "", err
	}

	fmt.Printf("OpenAI response: %v\n", resp.Choices[len(resp.Choices)-1].Message.Content)

	// Process the response and function call
	msg := resp.Choices[0].Message

	fmt.Printf("OpenAI answered the original request with: %v\n", msg.Content)
	return msg.Content, nil
}
