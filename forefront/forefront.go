package forefront

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/pkoukk/tiktoken-go"
	"gpt-4-proxy/conf"
	"gpt-4-proxy/openai"
	"gpt-4-proxy/util"
	"io"
	"log"
	"strings"
)

type ForeFrontClient struct {
	UserId      string `json:"user_id"`
	SessionId   string `json:"session_id"`
	JWT         string `json:"jwt"`
	Lock        bool   `json:"lock"`
	WorkSpaceId string `json:"work_space_id"`
}

type Message struct {
	ChatId  string `json:"chatId"`
	Content string `json:"content"`
	Role    string `json:"role"`
	Seq     int    `json:"seq"`
}

var ForeFrontClientList []ForeFrontClient

func InitForeFrontClients() {
	ForeFrontClientList = make([]ForeFrontClient, 0)
	for userId, sessionId := range conf.Conf.ForeSession {
		client := ForeFrontClient{
			UserId:      userId,
			SessionId:   sessionId,
			JWT:         conf.Conf.ForeJwt[userId],
			WorkSpaceId: conf.Conf.ForeChatId[userId],
			Lock:        false,
		}
		ForeFrontClientList = append(ForeFrontClientList, client)
	}
}

func GetForeClient() *ForeFrontClient {
	for _, client := range ForeFrontClientList {
		if !client.Lock {
			client.Lock = true
			return &client
		}
	}
	return nil
}

func ReleaseClient(client ForeFrontClient) {
	client.Lock = false
}

func GetContentToSend(messages []openai.Message) string {
	leadingMap := map[string]string{
		"system":    "YouShouldKnow",
		"user":      "Me",
		"assistant": "You",
		"function":  "Information",
	}
	content := ""

	for _, message := range messages {
		content += "||>" + leadingMap[message.Role] + ":\n" + message.Content + "\n"
	}
	content += "||>You:\n"

	//util.Logger.Debug("Generated content to send: " + content)
	return content
}

func Stream(messages []openai.Message, client ForeFrontClient, model string) (<-chan string, int, error) {
	var useModel string
	if strings.HasPrefix(model, "gpt-4") {
		useModel = "gpt-4"
	} else if strings.HasPrefix(model, "gpt-3") {
		useModel = "gpt-3.5-turbo"
	}
	tkm, err := tiktoken.EncodingForModel("gpt-4")
	if err != nil {
		err = fmt.Errorf("getEncoding: %v", err)
		return nil, 0, err
	}
	data := client.UserId + "default" + client.WorkSpaceId
	encodedData := base64.StdEncoding.EncodeToString([]byte(data))
	encryptedData := util.Encrypt(encodedData, client.SessionId)

	headers := map[string]string{
		"authority":          "streaming-worker.forefront.workers.dev",
		"accept":             "*/*",
		"accept-language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
		"cache-control":      "no-cache",
		"content-type":       "application/json",
		"origin":             "https://chat.forefront.ai",
		"pragma":             "no-cache",
		"referer":            "https://chat.forefront.ai/",
		"sec-ch-ua":          `"Not/A)Brand";v="99", "Microsoft Edge";v="115", "Chromium";v="115"`,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"macOS"`,
		"sec-fetch-dest":     "emty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "cross-site",
		"authorization":      "Bearer " + client.JWT,
		"X-Signature":        encryptedData,
		"user-agent":         `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36 Edg/115.0.1901.183`,
	}
	text := GetContentToSend(messages)
	token := tkm.Encode(text, nil, nil)
	jsonMap := map[string]interface{}{
		"text":           text,
		"id":             util.RandStringRunes2(9),
		"action":         "new",
		"parentId":       client.WorkSpaceId,
		"workspaceId":    client.WorkSpaceId,
		"messagePersona": "default",
		"model":          useModel,
		"internetMode":   "never",
		"messages":       map[string]interface{}{},
		"hidden":         false,
	}

	url := "https://streaming-worker.forefront.workers.dev/chat"

	jsonData, err := json.Marshal(jsonMap)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, 0, err
	}
	log.Println(jsonMap)
	req, err := fhttp.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	tlsClient, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), util.InitTlsClient()...)
	if err != nil {
		return nil, 0, err
	}
	resp, err := tlsClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	msgChannel := make(chan string)
	go StreamConversation(resp, msgChannel, model)
	return msgChannel, len(token), nil
}

func StreamConversation(response *fhttp.Response, textChan chan string, model string) {
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}

		}
		//log.Println(line)
		//if len(line) < 8 || line == "" {
		//	continue
		//}
		if strings.HasPrefix(line, "event: end") {
			break
		}
		if strings.HasPrefix(line, "event: ") {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			if strings.HasPrefix(line, "data: {") {
				jsonMap := make(map[string]string)
				jsonStr := line[6:]
				//log.Println(jsonStr)
				err = json.Unmarshal([]byte(jsonStr), &jsonMap)
				if err != nil {
					log.Println(err)
					continue
				}
				fmt.Printf(jsonMap["delta"])
				textChan <- jsonMap["delta"]
			} else {
				break
			}
		}
	}
	log.Println()
	log.Println("streaming finished")
	textChan <- "[DONE]"
	close(textChan)
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
