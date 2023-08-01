package main

import (
	"github.com/gin-gonic/gin"
	conf "gpt-4-proxy/conf"
	"gpt-4-proxy/forefront"
	"gpt-4-proxy/router"
	"strconv"
)

func main() {
	conf.Setup()
	engine := gin.Default()
	forefront.InitForeFrontClients()
	router.Setup(engine)
	err := engine.Run(":" + strconv.Itoa(conf.Conf.Port))
	if err != nil {
		panic(err)
	}
}
