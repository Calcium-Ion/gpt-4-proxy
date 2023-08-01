package util

import (
	tls_client "github.com/bogdanfinn/tls-client"
	"math/rand"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var letterRunes2 = []rune("abcdefghijklmnopqrstuvwxyz1234567890")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func RandStringRunes2(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes2[rand.Intn(len(letterRunes2))]
	}
	return string(b)
}

func InitTlsClient() []tls_client.HttpClientOption {
	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(tls_client.Okhttp4Android13),
		// tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(jar), // create cookieJar instance and pass it as argument
	}
	return options
}
