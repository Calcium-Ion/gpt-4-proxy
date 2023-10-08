package forefront

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/gin-gonic/gin"
	"github.com/pkoukk/tiktoken-go"
	"gpt-4-proxy/conf"
	"gpt-4-proxy/exception"
	"gpt-4-proxy/openai"
	"gpt-4-proxy/util"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	UserId    string `json:"user_id"`
	SessionId string `json:"session_id"`
	ClientJwt string `json:"jwt"`
	TempJwt   string `json:"temp_jwt"`
	Sign      string `json:"sign"`
	//Lock        bool   `json:"lock"`
	ChatCount   int    `json:"chat_count"`
	WorkSpaceId string `json:"work_space_id"`
	TlsClient   tls_client.HttpClient
}

type Message struct {
	ChatId  string `json:"chatId"`
	Content string `json:"content"`
	Role    string `json:"role"`
	Seq     int    `json:"seq"`
}

var ClientList []*Client

func InitForeFrontClients() {
	ClientList = make([]*Client, 0)
	for userId, sessionId := range conf.Conf.ForeSession {
		jar := tls_client.NewCookieJar()
		options := []tls_client.HttpClientOption{
			tls_client.WithTimeoutSeconds(300),
			tls_client.WithClientProfile(tls_client.Okhttp4Android13),
			// tls_client.WithNotFollowRedirects(),
			tls_client.WithCookieJar(jar), // create cookieJar instance and pass it as argument
		}
		tlsClient, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
		if err != nil {
			log.Fatal(err)
		}
		client := Client{
			UserId:      userId,
			SessionId:   sessionId,
			ClientJwt:   conf.Conf.ForeJwt[userId],
			TempJwt:     "",
			WorkSpaceId: conf.Conf.ForeChatId[userId],
			Sign:        conf.Conf.ForeSign[userId],
			ChatCount:   0,
			TlsClient:   tlsClient,
		}
		ClientList = append(ClientList, &client)
	}

	go func() {
		//每一分钟检查一次
		for {
			for _, client := range ClientList {
				// url = https://clerk.forefront.ai/v1/client/sessions/%s/tokens?_clerk_js_version=4.56.3
				uri := fmt.Sprintf("https://clerk.forefront.ai/v1/client/sessions/%s/touch?_clerk_js_version=4.56.3", client.SessionId)
				headers := map[string]string{
					"accept":             "*/*",
					"accept-language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
					"cache-control":      "no-cache",
					"content-type":       "application/x-www-form-urlencoded",
					"cookie":             fmt.Sprintf("__client=%s", client.ClientJwt),
					"origin":             "https://www.forefront.ai",
					"pragma":             "no-cache",
					"referer":            "https://www.forefront.ai/",
					"sec-ch-ua":          `"Chromium";v="116", "Not)A;Brand";v="24", "Microsoft Edge";v="116"`,
					"sec-ch-ua-mobile":   "?0",
					"sec-ch-ua-platform": `"macOS"`,
					"sec-fetch-dest":     "empty",
					"sec-fetch-mode":     "cors",
					"sec-fetch-site":     "cross-site",
					"user-agent":         `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36 Edg/116.0.1938.54`,
				}
				req, err := fhttp.NewRequest("POST", uri, nil)
				for key, value := range headers {
					req.Header.Set(key, value)
				}
				log.Println(req.Header)
				resp, err := client.TlsClient.Do(req)
				if err != nil {
					log.Println(err)
					continue
				}
				defer resp.Body.Close()
				bodyBytes, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Println(err)
					continue
				}
				//print string
				log.Println(string(bodyBytes))
				var result map[string]interface{}
				err = json.Unmarshal(bodyBytes, &result)
				if err != nil {
					log.Println(err)
					continue
				}
				clientJson := result["client"].(map[string]interface{})
				sessionArray := clientJson["sessions"].([]interface{})
				session0 := sessionArray[0].(map[string]interface{})
				latJson := session0["last_active_token"].(map[string]interface{})
				jwt := latJson["jwt"].(string)
				log.Println(jwt)
				//更新jwt
				client.TempJwt = jwt
			}
			time.Sleep(50 * time.Second)
		}
	}()
}

var lastIndex = 0

func GetForeClient(count int) []*Client {
	if count == 0 {
		count = 1
	}
	clients := make([]*Client, 0)
	for i := 0; i < count; i++ {
		client := ClientList[lastIndex]
		lastIndex = (lastIndex + 1) % len(ClientList)
		if client.ChatCount < conf.Conf.MaxThreads {
			client.ChatCount++
			clients = append(clients, client)
		} else {
			for _, client := range ClientList {
				if client.ChatCount < conf.Conf.MaxThreads {
					client.ChatCount++
					clients = append(clients, client)
				}
			}
		}
	}
	return clients
}

func ReleaseClient(client *Client) {
	client.ChatCount--
}

func GetContentToSend(messages []openai.Message) string {
	leadingMap := map[string]string{
		"system":    "Instructions",
		"user":      "User",
		"assistant": "Assistant",
		"function":  "Information",
	}
	content := ""

	if len(messages) == 1 {
		content += messages[0].Content
	} else {
		for _, message := range messages {
			content += "||>" + leadingMap[message.Role] + ":\n" + message.Content + "\n"
		}
		content += "||>Assistant:\n"
	}

	//util.Logger.Debug("Generated content to send: " + content)
	return content
}

func Ask(c *gin.Context, messages []openai.Message, client Client, model string) (<-chan string, int, error) {
	chans := make(chan string, 1)
	resp, token, err := Stream(c, messages, client, model)
	if err != nil {
		return nil, 0, err
	}
	go func() {
		timeout := time.Duration(20) * time.Second
		maxResponseTime := time.Duration(3) * time.Minute
		ticker := time.NewTimer(timeout)
		maxTicker := time.NewTimer(maxResponseTime)
		defer ticker.Stop()
		defer maxTicker.Stop()
		content := ""
		//for m := range forefront.GetDataStream(resp) {
		//	content += m
		//}
		for {
			select {
			case <-ticker.C:
				log.Println("stream timeout")
				goto END
			case <-maxTicker.C:
				log.Println("stream max timeout")
				goto END
			case m := <-resp:
				ticker.Reset(timeout)
				if m != "[DONE]" {
					if m == "[ERROR]" {
						time.Sleep(1 * time.Second)
						if len(content) == 0 {
							content = "[ERROR]"
						}
						goto END
					}
					if m == "" {
						continue
					}
					content += m
				} else {
					goto END
				}
			}
		}
	END:
		chans <- content
	}()
	return chans, token, nil
}

func Stream(c *gin.Context, messages []openai.Message, client Client, model string) (<-chan string, int, error) {
	net := "never"
	var useModel string
	if strings.HasPrefix(model, "gpt-4") {
		useModel = "gpt-4"
	} else if strings.HasPrefix(model, "gpt-3") {
		useModel = "gpt-3.5-turbo"
	} else if strings.HasPrefix(model, "claude") {
		if strings.HasPrefix(model, "claude-2") {
			useModel = "claude"
		} else {
			useModel = "claude-instant"
		}
	} else {
		return nil, 0, fmt.Errorf("unsupported model: %s", model)
	}
	if strings.Contains(model, "online") {
		net = "always"
	}
	tkm, err := tiktoken.EncodingForModel("gpt-4")
	if err != nil {
		err = fmt.Errorf("getEncoding: %v", err)
		return nil, 0, err
	}
	//second := time.Now().Unix()
	//data := client.SessionId
	////1b3a56a9e5c5f5274fcecc1d577a68140fdf3795ce274e046c6cd64ce2939db831e18bbedc2c2e9b985407c664946f6187315d7fe6d3d1272ffb8d2c781de44d
	////data := fmt.Sprintf("%s-%ddefault%s%d", client.UserId, second, client.WorkSpaceId, second)
	//encodedData := base64.StdEncoding.EncodeToString([]byte(data))
	//encryptedData := util.Encrypt(encodedData, client.SessionId)
	//log.Println("encryptedData: ", encryptedData)
	headers := map[string]string{
		"authority":          "streaming-worker.forefront.workers.dev",
		"accept":             "*/*",
		"accept-language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
		"cache-control":      "no-cache",
		"content-type":       "application/json",
		"origin":             "https://www.forefront.ai",
		"pragma":             "no-cache",
		"referer":            "https://www.forefront.ai/",
		"sec-ch-ua":          `"Chromium";v="116", "Not)A;Brand";v="24", "Microsoft Edge";v="116"`,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"macOS"`,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "cross-site",
		"authorization":      "Bearer " + client.TempJwt,
		"X-Signature":        client.Sign,
		"user-agent":         `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36 Edg/116.0.1938.54`,
	}
	text := "When addressing my inquiries, please utilize a more expansive line of thought.\n" + GetContentToSend(messages)
	token := tkm.Encode(text, nil, nil)
	if !strings.HasPrefix(model, "claude") {
		maxToken := 8191
		showMaxToken := 8192
		if strings.HasPrefix(model, "gpt-3.5") {
			if strings.Contains(model, "16k") {
				maxToken = 16383
				showMaxToken = 16384
			} else {
				maxToken = 6143
				showMaxToken = 4097
			}
		} else if strings.HasPrefix(model, "gpt-4") {
			if strings.Contains(model, "32k") {
				maxToken = 32767
				showMaxToken = 32768
			} else {
				maxToken = 8191
				showMaxToken = 8192
			}
		}
		if len(token) > maxToken {
			return nil, 0, &exception.InvalidRequestError{
				Message: fmt.Sprintf("This model's maximum context length is %d tokens, However, your messages resulted in %d tokens. Please reduce the length of the messages.", showMaxToken, len(token)),
			}
		}
	}
	log.Printf("tokens: %d", len(token))

	jsonMap := map[string]interface{}{
		"text":           text,
		"id":             util.RandStringRunes2(9),
		"action":         "new",
		"parentId":       client.WorkSpaceId,
		"workspaceId":    client.WorkSpaceId,
		"messagePersona": "default",
		"model":          useModel,
		"internetMode":   net,
		"messages":       map[string]interface{}{},
		"hidden":         true,
	}

	url := "https://streaming-worker.forefront.workers.dev/chat"
	log.Printf("body: %s", jsonMap)
	jsonData, err := json.Marshal(jsonMap)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, 0, err
	}
	//log.Println(jsonMap)
	req, err := fhttp.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	//tlsClient, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), util.InitTlsClient()...)
	//if err != nil {
	//	return nil, 0, err
	//}
	resp, err := client.TlsClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		log.Printf("resp body: %s", respBody)
		return nil, 0, &exception.InvalidRequestError{
			Message: fmt.Sprintf("bad status code: %d", resp.StatusCode),
		}
	}
	//log.Printf("resp headers: %v", resp.Header)
	msgChannel := make(chan string)
	go StreamConversation(c, resp, msgChannel, model)
	return msgChannel, len(token), nil
}

func StreamConversation(c *gin.Context, response *fhttp.Response, textChan chan string, model string) {
	defer response.Body.Close()
	defer close(textChan)
	reader := bufio.NewReader(response.Body)
	claudeLastResponse := ""
	respCount := 0
	isError := false
	timeout := time.Duration(20) * time.Second
	ticker := time.NewTimer(timeout)
	maxResponseTime := time.Duration(4) * time.Minute
	maxTicker := time.NewTimer(maxResponseTime)
	defer maxTicker.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			log.Println("streaming timeout")
			goto END
		case <-maxTicker.C:
			log.Println("streaming max timeout")
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					goto END
				}
			}
			//if len(line) > 1 {
			//	log.Println(line)
			//}
			if len(line) < 1 || line == "" {
				continue
			}
			if strings.HasPrefix(line, "event: end") {
				goto END
			}
			if strings.HasPrefix(line, "data: ") {
				ticker.Reset(timeout)
				if strings.HasPrefix(line, "data: {") {
					jsonMap := make(map[string]string)
					jsonStr := line[6:]
					//log.Println(jsonStr)
					err = json.Unmarshal([]byte(jsonStr), &jsonMap)
					if err != nil {
						log.Printf("err str: %s, model: %s, status: %d, respCount: %d", jsonStr, model, response.StatusCode, respCount)
						log.Println(err)
						if strings.Contains(jsonStr, "error") {
							if respCount == 0 {
								isError = true
							}
							goto END
						}
						continue
					}
					responseStr := ""
					if strings.HasPrefix(model, "claude") {
						temp := jsonMap["text"]
						if len(temp) < 4 && strings.Contains(temp, "�") {
							continue
						} else {
							temp = strings.ReplaceAll(temp, "��", "�")
						}
						if len(temp) < len(claudeLastResponse) {
							continue
						}
						responseStr = temp[len(claudeLastResponse):]
						claudeLastResponse = temp
					} else {
						responseStr = jsonMap["delta"]
					}
					//fmt.Printf(responseStr)
					textChan <- responseStr
					if responseStr != "" {
						respCount++
					}
				} else {
					goto END
				}
			}
		}
	}
END:
	if isError {
		log.Println("streaming error")
		textChan <- "[ERROR]"
	} else {
		log.Println("streaming finished")
		textChan <- "[DONE]"
	}
	//close(textChan)
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
