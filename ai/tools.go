package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

type MathResponse struct {
	Equation string `json:"equation"`
	Context  string `json:"context"`
	Value    string
}

// Creates mathematical expressions or calculations.
func (c *Client) GenerateMath(expression string) (result MathResponse, err error) {
	ctx := context.Background()

	var schema = openai.ChatCompletionResponseFormatJSONSchema{
		Name:        "generateMathJavascript",
		Description: "Generate a Javascript/es6 expression to calculate the result of a mathematical expression.",
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"equation": {
					Type:        jsonschema.String,
					Description: `returns Javascript/es6 code that will be executed calculate the results from the users input. dont add any formatting or comments`,
				},
				"context": {
					Type:        jsonschema.String,
					Description: "An explanation of why this tool is needed step by step.",
				},
			},
			Required:             []string{"equation", "context"},
			AdditionalProperties: false,
		},
		Strict: true,
	}

	// Prepare the initial user message
	dialogue := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: fmt.Sprintln(`I can help you write a Javascript/es6 IIFE that will calculate the result of a mathematical expression. I will return the value.`),
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: expression,
		},
	}

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
		return MathResponse{}, err
	}

	fmt.Printf("OpenAI response: %v\n", resp.Choices[len(resp.Choices)-1].Message.Content)

	// Process the response and function call
	msg := resp.Choices[0].Message

	var mathResponse MathResponse
	err = json.Unmarshal([]byte(msg.Content), &mathResponse)
	if err != nil {
		fmt.Println("Error unmarshaling JSON")
	}

	// Execute the python code
	mathResults, err := ExecuteMath(mathResponse.Equation)
	if err != nil {
		fmt.Println("Error executing python code")
	}

	mathResponse.Value = mathResults

	fmt.Printf("OpenAI answered the original request with: %v\n", msg.Content)
	return mathResponse, nil
}

// ExecuteMath executes a Node.js script and returns the result
func ExecuteMath(expression string) (result string, err error) {
	// Construct the Node.js code with a console.log to output the result
	nodeCode := fmt.Sprintf(`
        try {
            console.log(
                %s
            );
        } catch (error) {
            console.error(error);
     		process.exit(1);
        }
    `, expression)

	// Open Node.js process with Go stdlib
	node := exec.Command("node", "-e", nodeCode)

	// Execute the command and capture the output
	out, err := node.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error executing Node.js code: %v\nOutput: %s", err, out)
	}

	// Trim any whitespace from the output
	result = strings.TrimSpace(string(out))

	fmt.Printf("Output from the Node.js process: %s\n", result)

	return result, nil
}

// // Generates an API call based on the provided parameters.
// func (c *Client) GenerateApi(params map[string]interface{}) (string, error) {
// 	// Implementation here
// }

// // Executes the API call and retrieves the data.
// func (c *Client) CallApi(apiCall string) (responseBody string, err error) {
// 	// Implementation here
// }

// // Parses the response received from the API call into a usable format.
// func (c *Client) ParseResponse(responseBody string) (parsedData interface{}, err error) {
// 	// Implementation here
// }

type DisplayResponse struct {
	Markup  string `json:"markup"`
	Context string `json:"context"`
}

// Generates the HTML structure needed to display the parsed data.
func (c *Client) GenerateDisplayHtml(displayContext string) (displayContextStruct DisplayResponse, err error) {
	ctx := context.Background()

	var schema = openai.ChatCompletionResponseFormatJSONSchema{
		Name:        "GenerateDisplayHtml",
		Description: "Generate the HTML structure needed to display data in the user prompt",
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"markup": {
					Type:        jsonschema.String,
					Description: `returns the HTML structure needed to display the parsed data from the user prompt`,
				},
				"context": {
					Type:        jsonschema.String,
					Description: "An explanation of why this tool is needed step by step.",
				},
			},
			Required:             []string{"markup", "context"},
			AdditionalProperties: false,
		},
		Strict: true,
	}

	// Prepare the initial user message
	dialogue := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintln(`You are an AI designed to generate the contents of the <body> tag of an HTML document. Your task is to create a body section that utilizes the following external resources. Ensure that the content is visually appealing and functional according to the user's specifications.

			Markup Rules:
			HTML structure must be valid and semantically correct.
			Use the specified external resources for styling, functionality, and content rendering.
			Ensure that the content is responsive and visually appealing.
			Do not include additional scripts or resources beyond the specified ones.
			DO NOT include the <head> tag or any meta tags in the generated content.
			Do NOT include any server-side code or backend functionality.
			Do not include any CSS links or stylesheets in the content.
			Must Not Include: head tag, meta tags, CSS links, stylesheets, server-side code, backend functionality,script tags with sources.
			
			Code RULES:
			Code must be browser only, no server-side code.
			Code must be written in JavaScript with es6 syntax.
			COde must be only use the specified external resources.
			Code must be optimized for performance and efficiency.
			Code must use the lowest amount of characters possible.
			Code must wait always Defer attribute on script tags.
			Code must never import any external libraries or scripts.

			External Resources:

			Tailwind CSS for styling.
			Marked.js for Markdown parsing.
			Toastify.js for toast notifications.
			Mermaid.js for diagram generation.
			Highlight.js for syntax highlighting.
			Chart.js for charting and data visualization.
			Three.js for 3D graphics.
			React for building interactive UIs.
			React DOM for rendering React components.
			HTM for writing React components with HTML-like syntax.
			Prompt to Generate Body Content:

			Create the contents of the <body> tag with the following requirements:

			Structure:

			Include a clean and responsive layout using Tailwind CSS.
			Incorporate sections for different functionalities:
			Markdown Content: Render Markdown content using Marked.js.
			Diagrams: Display Mermaid.js diagrams.
			Charts: Visualize data with Chart.js charts.
			3D Graphics: Render 3D graphics using Three.js.
			Interactive UIs: Use React and HTM to build interactive components and dynamic UIs.
			Styling:

			Use Tailwind CSS classes to style the page content.
			Ensure the content is visually appealing and adheres to modern design principles.
			Functionality:

			Implement interactive UIs with React and HTM.
			Add a button or element that triggers a toast notification using Toastify.js.
			Include a code block with syntax highlighting using Highlight.js.
			Provide interactive elements for Markdown content, diagrams, charts, and 3D graphics.

			JavaScript Integration:

			Ensure that the content integrates and leverages the external scripts effectively.
			Use Marked.js for Markdown rendering.
			Initialize Mermaid.js for diagrams.
			Configure and display charts with Chart.js.
			Set up and render a 3D scene with Three.js.
			Build and render interactive UIs using React and HTM.

			User Input:

			The user will provide additional details or preferences for the page layout, content, or design. Make sure to incorporate these specifics into the body content.

			// Function to show toast notifications
				const showToast = (message, type = "info") => {
					Toastify({
					text: message,
					duration: 3000,
					gravity: "top",
					position: "right",
					backgroundColor:
						type === "error"
						? "#ff6b6b"
						: type === "warning"
						? "#feca57"
						: type === "success"
						? "#48dbfb"
						: "#54a0ff",
					stopOnFocus: true,
					}).showToast();
				};
		`),
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: displayContext,
		},
	}

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
		return DisplayResponse{}, err
	}

	fmt.Printf("OpenAI response: %v\n", resp.Choices[len(resp.Choices)-1].Message.Content)

	// Process the response and function call
	msg := resp.Choices[0].Message

	var displayResponse DisplayResponse
	err = json.Unmarshal([]byte(msg.Content), &displayResponse)
	if err != nil {
		fmt.Println("Error unmarshaling JSON")
	}

	fmt.Printf("OpenAI answered the original request with: %v\n", msg.Content)
	return displayResponse, nil
}

// // Generates the final output in the chat format, ready for display.
// func (c *Client) GenerateOutput(parsedData interface{}) (chatFormat string, err error) {
// 	// Implementation here
// }

// // Stores intermediate results to optimize multistage processes.
// func (c *Client) CacheResults(key string, data interface{}) error {
// 	// Implementation here
// }

// // Retrieves data from a database.
// func (c *Client) FetchDatabase(query string) (data interface{}, err error) {
// 	// Implementation here
// }

// // Saves data into a database.
// func (c *Client) StoreData(data interface{}) error {
// 	// Implementation here
// }

// // Processes raw data into meaningful information.
// func (c *Client) ProcessData(rawData interface{}) (processedData interface{}, err error) {
// 	// Implementation here
// }

// // Filters data based on specific criteria.
// func (c *Client) FilterData(data interface{}, criteria map[string]interface{}) (filteredData interface{}, err error) {
// 	// Implementation here
// }

// // Sorts data in ascending or descending order.
// func (c *Client) SortData(data interface{}, ascending bool) (sortedData interface{}, err error) {
// 	// Implementation here
// }

// // Transforms data into a different format or structure.
// func (c *Client) TransformData(data interface{}, format string) (transformedData interface{}, err error) {
// 	// Implementation here
// }

// // Generates reports from processed data.
// func (c *Client) GenerateReport(processedData interface{}) (report string, err error) {
// 	// Implementation here
// }

// // Sends an email with the generated report or other data.
// func (c *Client) SendEmail(to string, subject string, body string) error {
// 	// Implementation here
// }

// // Creates visual graphs from data.
// func (c *Client) GenerateGraph(data interface{}) (graphImage []byte, err error) {
// 	// Implementation here
// }

// // Executes a script or a sequence of commands.
// func (c *Client) ExecuteScript(script string) (output string, err error) {
// 	// Implementation here
// }

// // Logs the activities performed during the execution plan.
// func (c *Client) LogActivity(activity string) error {
// 	// Implementation here
// }
