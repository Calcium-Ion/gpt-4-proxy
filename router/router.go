package router

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pkoukk/tiktoken-go"
	"gpt-4-proxy/forefront"
	"gpt-4-proxy/util"

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
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, "bad request")
			return
		}
		for _, msg := range req.Messages {
			if msg.Role != "system" && msg.Role != "user" && msg.Role != "assistant" {
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
	client := forefront.GetForeClient()
	defer forefront.ReleaseClient(*client)
	if client == nil {
		openAIError := openai.OpenAIError{
			Type:    "openai_api_error",
			Message: "The server is overload",
			Code:    "do_request_failed",
		}
		c.JSON(500, gin.H{
			"error": openAIError,
		})
	}
	tkm, err := tiktoken.EncodingForModel("gpt-4")
	if err != nil {
		err = fmt.Errorf("getEncoding: %v", err)
		panic(err)
		return
	}
	conversationID := "chatcmpl-" + util.RandStringRunes(29)
	resp, token, err := forefront.Stream(req.Messages, *client, req.Model)
	if err != nil {
		panic(err)
		return
	}
	timeout := time.Duration(10) * time.Second
	ticker := time.NewTimer(timeout)
	defer ticker.Stop()
	content := ""
	//for m := range forefront.GetDataStream(resp) {
	//	content += m
	//}
	for {
		select {
		case <-ticker.C:
			log.Println("stream timeout")
			goto END
		case m := <-resp:
			ticker.Reset(timeout)
			if m != "[DONE]" {
				content += m
			} else {
				goto END
			}
		}
	}
END:
	choice := openai.Choice{
		Index: 0,
		Message: openai.Message{
			Content: content,
			Role:    "assistant",
		},
		FinishReason: "stop",
	}
	completionTokens := tkm.Encode(content, nil, nil)
	sse := openai.CompletionResponse{
		Choices: []openai.Choice{choice},
		Created: time.Now().Unix(),
		ID:      conversationID,
		Model:   req.Model,
		Object:  "chat.completion",
		Usage: openai.Usage{
			PromptTokens:     token,
			CompletionTokens: len(completionTokens),
			TotalTokens:      token + len(completionTokens),
		},
	}
	//dataV, _ := json.Marshal(&sse)
	c.JSON(200, sse)
}

func Stream(c *gin.Context, req openai.CompletionRequest) {
	log.Println("streaming started")
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
			openAIError := openai.OpenAIError{
				Type:    "openai_api_error",
				Message: "The server had an error while processing your request",
				Code:    "do_request_failed",
			}
			c.JSON(500, gin.H{
				"error": openAIError,
			})
		}
	}()
	client := forefront.GetForeClient()
	defer forefront.ReleaseClient(*client)
	if client == nil {
		openAIError := openai.OpenAIError{
			Type:    "openai_api_error",
			Message: "The server is overload",
			Code:    "do_request_failed",
		}
		c.JSON(500, gin.H{
			"error": openAIError,
		})
	}
	resp, _, err := forefront.Stream(req.Messages, *client, req.Model)
	if err != nil {
		panic(err)
		return
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	conversationID := "chatcmpl-" + util.RandStringRunes(29)

	w := c.Writer
	flusher, _ := w.(http.Flusher)
	timeout := time.Duration(10) * time.Second
	ticker := time.NewTimer(timeout)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			log.Println("stream timeout")
			goto END
		case m := <-resp:
			ticker.Reset(timeout)
			if m != "[DONE]" {
				sse := GenSSEResponse(m, conversationID, nil, req.Model)
				dataV, _ := json.Marshal(&sse)
				_, err := io.WriteString(w, "data: "+string(dataV)+"\n\n")
				if err != nil {
					log.Println(err)
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
	//for m := range forefront.GetDataStream(resp) {
	//
	//	if m != "[DONE]" {
	//		sse := GenSSEResponse(m, conversationID, nil, req.Model)
	//		dataV, _ := json.Marshal(&sse)
	//		_, err := io.WriteString(w, "data: "+string(dataV)+"\n\n")
	//		if err != nil {
	//			log.Println(err)
	//		}
	//		flusher.Flush()
	//	} else {
	//		str := "stop"
	//		sse := GenSSEResponse("", conversationID, &str, req.Model)
	//		dataV, _ := json.Marshal(&sse)
	//		_, err := io.WriteString(w, "data: "+string(dataV)+"\n\n")
	//		if err != nil {
	//			log.Println(err)
	//		}
	//		flusher.Flush()
	//		break
	//	}
	//}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
	log.Println("stream closed")
}

func GenSSEResponse(text string, conversationID string, finishReason *string, model string) openai.CompletionSSEResponse {
	delta := map[string]string{}
	if finishReason == nil {
		delta["content"] = text
		delta["role"] = "assistant"
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
