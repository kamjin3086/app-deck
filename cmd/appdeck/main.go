package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/kamjin/app-deck/internal/catalog"
	"github.com/kamjin/app-deck/internal/server"
	"github.com/kamjin/app-deck/web"
)

func main() {
	defaultDir, err := catalog.DefaultConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	host := flag.String("host", "127.0.0.1", "listen host")
	port := flag.String("port", "8788", "listen port")
	config := flag.String("config", filepath.Join(defaultDir, "appdeck.json"), "preferences file")
	flag.Parse()

	if err := server.EnsureInitialConfig(*config); err != nil {
		log.Fatalf("init config: %v", err)
	}
	app, err := server.New(*config, web.FS)
	if err != nil {
		log.Fatalf("start appdeck: %v", err)
	}
	addr := server.ListenAddr(*host, *port)
	fmt.Printf("AppDeck listening at http://%s\n", addr)
	if err := http.ListenAndServe(addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}
