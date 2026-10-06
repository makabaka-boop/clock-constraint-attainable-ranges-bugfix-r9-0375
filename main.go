package main

import (
	"log"
	"net/http"
	"os"

	"clocksync/internal/api"
	"clocksync/web"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("时钟偏移校正复核服务监听于 %s", addr)
	log.Fatal(http.ListenAndServe(addr, api.New(web.Dist())))
}
