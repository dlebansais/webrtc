package main

import (
	"flag"
	"fmt"
	"net/http"
)

func main() { // nolint:gocognit
	answerAddr := flag.String("answer-address", ":22570", "Address that the Answer HTTP server is listening on.")
	flag.Parse()

	fmt.Printf("Answer address: %s\n", *answerAddr)

	fmt.Printf("Starting server\n")

	// Start HTTP server that accepts requests from the offer process to exchange SDP and Candidates
	panic(http.ListenAndServe(*answerAddr, nil))
}
