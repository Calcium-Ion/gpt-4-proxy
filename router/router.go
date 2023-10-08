package router

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gpt-4-proxy/exception"
	"gpt-4-proxy/forefront"
	"gpt-4-proxy/util"
	"sync"

	//api "gpt-4-proxy/jetbrain"
	"gpt-4-proxy/openai"
	"io"
	"log"
	"net/http"
	"time"
)

func SetCORS(c *gin.Context) {
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	c.Writer.Header().Set("Access-Control-Max-Age", "86400")
	c.Writer.Header().Set("Content-Type", "application/json")
}

func Setup(engine *gin.Engine) {
	postCompletions := func(c *gin.Context) {
		SetCORS(c)
		var req openai.CompletionRequest
		if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
			log.Printf("CompletionRequestError: %v", err)
			c.JSON(400, "bad request")
			return
		}
		for _, msg := range req.Messages {
			if msg.Role != "system" && msg.Role != "user" && msg.Role != "assistant" && msg.Role != "function" {
				log.Printf("error: %v data: %v", errors.New("role of message validation failed"), c.Request.Body)
				c.JSON(400, "role of message validation failed: "+msg.Role)
				return
			}
		}

		if req.Stream {
			Stream(c, req)
		} else {
			Ask(c, req)
		}
	}

	engine.POST("/chat/completions", postCompletions)
	engine.POST("/v1/chat/completions", postCompletions)
}

func Ask(c *gin.Context, req openai.CompletionRequest) {
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
			openAIError := openai.OpenAIError{
				Type:    "openai_api_error",
				Message: "The server had an error while processing your request",
				Code:    nil,
			}
			c.JSON(500, gin.H{
				"error": openAIError,
			})
		}
	}()
	clients := forefront.GetForeClient(req.N)
	if len(clients) == 0 {
		openAIError := openai.OpenAIError{
			Type:    "server_error",
			Message: "That model is currently overloaded with other requests. You can retry your request, or contact us through our help center at help.openai.com if the error persists.",
			Code:    nil,
		}
		c.JSON(503, gin.H{
			"error": openAIError,
		})
		log.Printf("no clients available, model: %s", req.Model)
		return
	} else if len(clients) < req.N || req.N > 10 {
		openAIError := openai.OpenAIError{
			Type:    "server_error",
			Message: "That model is currently overloaded with other requests. You can retry your request, or contact us through our help center at help.openai.com if the error persists.",
			Code:    nil,
		}
		c.JSON(503, gin.H{
			"error": openAIError,
		})
		log.Printf("not enough n clients available, model: %s", req.Model)
		return
	}
	promptTokens := 0
	completionTokens := 0
	isError := false
	count := 1
	if req.N > 1 {
		count = req.N
	}
	choices := make([]openai.Choice, count)
	var wg sync.WaitGroup
	for i, client := range clients {
		wg.Add(1)
		go func(index int, client *forefront.Client, wg *sync.WaitGroup) {
			log.Printf("asking using %s, model: %s", clients[0].UserId, req.Model)
			defer forefront.ReleaseClient(client)
			defer wg.Done()
			askChan, tkm, err := forefront.Ask(c, req.Messages, *client, req.Model)
			if err != nil {
				log.Println(err)
				if err == err.(*exception.InvalidRequestError) {
					openAIError := openai.OpenAIError{
						Type:    "invalid_request_error",
						Message: err.Error(),
						Code:    nil,
					}
					c.JSON(503, gin.H{
						"error": openAIError,
					})
					isError = true
				}
				//panic(err)
				return
			}
			completion := <-askChan
			if completion == "[ERROR]" {
				openAIError := openai.OpenAIError{
					Type:    "server_error",
					Message: "That model is currently overloaded with other requests. You can retry your request, or contact us through our help center at help.openai.com if the error persists.",
					Code:    nil,
				}
				c.AbortWithStatusJSON(503, gin.H{
					"error": openAIError,
				})
				isError = true
				return
			}
			choice := openai.Choice{
				Index: index,
				Message: openai.Message{
					Content: completion,
					Role:    "assistant",
				},
				FinishReason: "stop",
			}
			choices[index] = choice
			promptTokens = tkm
			completionTokens += len(completion)
		}(i, client, &wg)
	}
	wg.Wait()
	//dataV, _ := json.Marshal(&sse)
	if !isError {
		conversationID := "chatcmpl-" + util.RandStringRunes(29)
		model := req.Model
		if model == "gpt-4" {
			model = "gpt-4-0613"
		}
		sse := openai.CompletionResponse{
			Choices: choices,
			Created: time.Now().Unix(),
			ID:      conversationID,
			Model:   model,
			Object:  "chat.completion",
			Usage: openai.Usage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			},
		}
		c.JSON(200, sse)
	}
}

func Stream(c *gin.Context, req openai.CompletionRequest) {
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
			openAIError := openai.OpenAIError{
				Type:    "openai_api_error",
				Message: "The server had an error while processing your request",
				Code:    nil,
			}
			c.JSON(500, gin.H{
				"error": openAIError,
			})
		}
	}()
	clients := forefront.GetForeClient(1)
	if len(clients) == 0 {
		openAIError := openai.OpenAIError{
			Type:    "server_error",
			Message: "That model is currently overloaded with other requests. You can retry your request, or contact us through our help center at help.openai.com if the error persists.",
			Code:    nil,
		}
		c.JSON(503, gin.H{
			"error": openAIError,
		})
		return
	}
	defer forefront.ReleaseClient(clients[0])
	log.Printf("streaming using %s, model: %s", clients[0].UserId, req.Model)

	resp, _, err := forefront.Stream(c, req.Messages, *clients[0], req.Model)
	if err != nil {
		log.Println(err)
		if err == err.(*exception.InvalidRequestError) {
			openAIError := openai.OpenAIError{
				Type:    "invalid_request_error",
				Message: err.Error(),
				Code:    nil,
			}
			c.JSON(503, gin.H{
				"error": openAIError,
			})
			return
		}
		panic(err)
		return
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	conversationID := "chatcmpl-" + util.RandStringRunes(29)

	w := c.Writer
	flusher, _ := w.(http.Flusher)
	timeout := time.Duration(20) * time.Second
	ticker := time.NewTimer(timeout)
	isError := false
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			log.Println("stream timeout")
			goto END
		case m := <-resp:
			ticker.Reset(timeout)
			if m != "[DONE]" {
				if m == "[ERROR]" {
					time.Sleep(1 * time.Second)
					openAIError := openai.OpenAIError{
						Type:    "server_error",
						Message: "That model is currently overloaded with other requests. You can retry your request, or contact us through our help center at help.openai.com if the error persists.",
						Code:    nil,
					}
					c.AbortWithStatusJSON(503, gin.H{
						"error": openAIError,
					})
					isError = true
					goto END
				}
				if m == "" {
					continue
				}
				sse := GenSSEResponse(m, conversationID, nil, req.Model)
				dataV, _ := json.Marshal(&sse)
				_, err := io.WriteString(w, "data: "+string(dataV)+"\n\n")
				if err != nil {
					//log.Println("write err:" + err.Error())
				}
				flusher.Flush()
			} else {
				str := "stop"
				sse := GenSSEResponse("", conversationID, &str, req.Model)
				dataV, _ := json.Marshal(&sse)
				_, err := io.WriteString(w, "data: "+string(dataV)+"\n\n")
				if err != nil {
					log.Println(err)
				}
				flusher.Flush()
				goto END
			}
		}
	}
END:
	if !isError {
		_, err = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
	log.Println("stream closed")
}

func GenSSEResponse(text string, conversationID string, finishReason *string, model string) openai.CompletionSSEResponse {
	delta := map[string]string{}
	if finishReason == nil {
		delta["content"] = text
		delta["role"] = "assistant"
	}
	if model == "gpt-4" {
		model = "gpt-4-0613"
	}
	sse := openai.CompletionSSEResponse{
		Choices: []openai.SSEChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
		Created: time.Now().Unix(),
		Id:      conversationID,
		Model:   model,
		Object:  "chat.completion.chunk",
	}
	return sse
}
