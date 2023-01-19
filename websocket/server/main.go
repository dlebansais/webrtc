package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

func connect(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func main() { // nolint:gocognit
	serverAddr := flag.String("server-address", ":22570", "Address that the Answer HTTP server is listening on.")
	flag.Parse()

	log.SetFlags(0)

	log.Printf("Server address: %s\n", *serverAddr)

	http.HandleFunc("/connect", connect)

	log.Printf("Starting server\n")

	// Start HTTP server that accepts requests from the offer process to exchange SDP and Candidates
	log.Fatal(http.ListenAndServe(*serverAddr, nil))
}
