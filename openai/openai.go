package openai

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    any    `json:"code"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name"`
}
type JetBrainMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}
type CompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type CompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Created int64    `json:"created"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}
type CompletionSSEResponse struct {
	Choices []SSEChoice `json:"choices"`
	Created int64       `json:"created"`
	Id      string      `json:"id"`
	Model   string      `json:"model"`
	Object  string      `json:"object"`
}
type SSEChoice struct {
	Delta        map[string]string `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
	Index        int               `json:"index"`
}
type Delta struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
