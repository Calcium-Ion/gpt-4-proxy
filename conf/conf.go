package conf

import (
	"github.com/pelletier/go-toml/v2"
	"os"
)

type ConfigStruct struct {
	Port          int               `toml:"port"`
	CoolDown      int               `toml:"cool-down"`
	AutoReload    int               `toml:"auto-reload"`
	MaxThreads    int               `toml:"max-threads"`
	JetBrainToken string            `toml:"jetbrain-token"`
	ForeSession   map[string]string `toml:"fore-session"`
	ForeJwt       map[string]string `toml:"fore-jwts"`
	ForeChatId    map[string]string `toml:"fore-chatid"`
	ForeSign      map[string]string `toml:"fore-sign"`
}

var Conf ConfigStruct

func Setup() {
	v, err := os.ReadFile("config.toml")
	if err != nil {
		panic(err)
	}
	//log.Printf("%s", v)
	err = toml.Unmarshal(v, &Conf)
	if err != nil {
		panic(err)
	}
	if Conf.Port == 0 {
		Conf.Port = 3800
	}
	if Conf.CoolDown == 0 {
		Conf.CoolDown = 10
	}
}
