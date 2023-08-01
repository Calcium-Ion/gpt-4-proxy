package jetbrain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"gpt-4-proxy/conf"
	"gpt-4-proxy/openai"
	"gpt-4-proxy/util"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

var m sync.Mutex

func GetClient() bool {
	return m.TryLock()
}

func ReleaseClient() {
	m.Unlock()
}

func SendConversationRequest(messages []openai.Message) (chan string, error) {
	msgChannel := make(chan string)

	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(tls_client.Okhttp4Android13),
		// tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(jar), // create cookieJar instance and pass it as argument
	}

	headers := map[string]string{
		"Host":                     "forefront.app.prod.grazie.aws.intellij.net",
		"Accept":                   "text/event-stream",
		"Accept-Charset":           "UTF-8",
		"Cache-Control":            "no-cache",
		"Content-Type":             "application/json",
		"grazie-agent":             `{"name": "GoLand", "version": "2023.2"}`,
		"grazie-authenticate-jwt":  conf.Conf.JetBrainToken,
		"grazie-original-user-jwt": conf.Conf.JetBrainToken,
		"User-Agent":               "Ktor client",
	}
	url := "https://api.app.prod.grazie.aws.intellij.net/user/v5/llm/chat/stream/v3"
	var sendMessages []openai.JetBrainMessage

	for _, msg := range messages {
		var role string
		switch msg.Role {
		case "system":
			role = "System"
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		}
		sendMessages = append(sendMessages, openai.JetBrainMessage{
			Role: role,
			Text: msg.Content,
		})
	}

	data := map[string]interface{}{
		"chat": map[string]interface{}{
			"messages": sendMessages,
		},
		"profile": "openai-gpt-4",
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, err
	}
	req, err := fhttp.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	go DealConversation(resp, msgChannel)
	return msgChannel, nil
}

func DealConversation(response *fhttp.Response, textChan chan string) {
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	conversationID := "chatcmpl-" + util.RandStringRunes(29)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}

		}
		if len(line) < 6 {
			continue
		}
		line = line[6:]
		log.Println(line)
		if !strings.HasPrefix(line, "end") {
			jsonMap := make(map[string]string)
			err = json.Unmarshal([]byte(line), &jsonMap)
			if err != nil {
				log.Println(err)
				continue
			}
			data := GenSSEResponse(jsonMap["current"], conversationID, nil)
			dataV, _ := json.Marshal(&data)
			textChan <- "data: " + string(dataV) + "\n\n"
		} else {
			str := "stop"
			data := GenSSEResponse("", conversationID, &str)
			dataV, _ := json.Marshal(&data)
			textChan <- "data: " + string(dataV) + "\n\n"
			break
		}
	}
	log.Println("streaming finished")
	textChan <- "data: [DONE]\n\n"
	close(textChan)
}

func GenSSEResponse(text string, conversationID string, finishReason *string) openai.CompletionSSEResponse {
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
		Model:   "gpt-4",
		Object:  "chat.completion.chunk",
	}
	return sse
}

func GetDataStream(ch <-chan string) <-chan string {
	stream := make(chan string, 1)
	go func() {
		for message := range ch {
			stream <- message
		}
		close(stream)
	}()
	return stream
}
