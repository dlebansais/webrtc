package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

func main() { // nolint:gocognit
	answerAddr := flag.String("answer-address", ":22570", "Address that the Answer HTTP server is listening on.")
	flag.Parse()

	fmt.Printf("Answer address: %s\n", *answerAddr)

	// A HTTP handler that processes a connection request
	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Request on /connect\n")

		socketConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			panic(err)
		}

		go HandleClient(socketConn)

		fmt.Printf("Request on /connect has been handled\n")
	})

	fmt.Printf("Starting server\n")

	// Start HTTP server that accepts requests from the offer process to exchange SDP and Candidates
	panic(http.ListenAndServe(*answerAddr, nil))
}

func HandleClient(socketConn *websocket.Conn) {
	fmt.Printf("Handling client\n")

	localAddr := socketConn.LocalAddr()

	fmt.Printf("localAddr: %s\n", localAddr)

	socketConn.Close();

	fmt.Printf("Connection closed\n")
}
