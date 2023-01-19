package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/examples/internal/signal"
)

func signalCandidate(addr string, c *webrtc.ICECandidate) error {
	payload := []byte(c.ToJSON().Candidate)
	resp, err := http.Post(fmt.Sprintf("http://%s/candidate", addr), // nolint:noctx
		"application/json; charset=utf-8", bytes.NewReader(payload))
	if err != nil {
		return err
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		return closeErr
	}

	return nil
}

func main() { // nolint:gocognit
	answerAddr := flag.String("answer-address", ":22570", "Address that the Answer HTTP server is hosted on.")
	flag.Parse()

	fmt.Printf("Answer address: %s\n", *answerAddr)

	var candidatesMux sync.Mutex
	pendingCandidates := make([]*webrtc.ICECandidate, 0)

	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:vpn1.airtop.io:3478?transport=tcp"},
			},
			{
				URLs: []string{"turn:vpn1.airtop.io:8443?transport=udp"},
				Username: "JnE3qxanXcfLgYRm_server",
				Credential: "tbsC9AmnxRbW4edT_server",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
			{
				URLs: []string{"turn:vpn1.airtop.io:8443?transport=tcp"},
				Username: "JnE3qxanXcfLgYRm_server",
				Credential: "tbsC9AmnxRbW4edT_server",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
	}

	// Create a new RTCPeerConnection
	peerConnection, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	fmt.Printf("Peer connection created\n")

	// When an ICE candidate is available send to the other Pion instance
	// the other Pion instance will add this candidate by calling AddICECandidate
	peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}

		candidatesMux.Lock()
		defer candidatesMux.Unlock()

		desc := peerConnection.RemoteDescription()
		if desc == nil {
			fmt.Printf("Candidate received, no remote description\n")
		} else {
			//fmt.Printf("Candidate received\n******\n%s\n******\n", desc)
			fmt.Printf("Candidate received\n")

			pendingCandidates = append(pendingCandidates, c)
		}
	})

	// A HTTP handler that allows the other Pion instance to send us ICE candidates
	// This allows us to add ICE candidates faster, we don't have to wait for STUN or TURN
	// candidates which may be slower
	http.HandleFunc("/candidate", func(w http.ResponseWriter, r *http.Request) {
		candidate, candidateErr := ioutil.ReadAll(r.Body)
		if candidateErr != nil {
			panic(candidateErr)
		}

		fmt.Printf("Response Candidate received\n")

		if candidateErr := peerConnection.AddICECandidate(webrtc.ICECandidateInit{Candidate: string(candidate)}); candidateErr != nil {
			panic(candidateErr)
		}
	})

	// A HTTP handler that processes a SessionDescription given to us from the other Pion process
	http.HandleFunc("/sdp", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Request on /sdp\n")

		sdp := webrtc.SessionDescription{}
		if sdpErr := json.NewDecoder(r.Body).Decode(&sdp); sdpErr != nil {
			panic(sdpErr)
		}

		//fmt.Printf("SDP received:\n******\n%s\n******\n", sdp)
		fmt.Printf("SDP received\n")

		if sdpErr := peerConnection.SetRemoteDescription(sdp); sdpErr != nil {
			panic(sdpErr)
		}

		fmt.Printf("REMOTE DESCRIPTION SET\n")
		
		// Create an answer to send to the other process
		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			panic(err)
		}

		//fmt.Printf("Answer created:\n******%s\n******\n", answer)
		fmt.Printf("Answer created\n")

		// Sets the LocalDescription, and starts our UDP listeners
		err = peerConnection.SetLocalDescription(answer)
		if err != nil {
			panic(err)
		}

		fmt.Printf("LOCAL DESCRIPTION SET\n")

		fmt.Printf("Creating response\n")

		payload, err := json.Marshal(answer)
		if err != nil {
			panic(err)
		}
		
		w.Write(payload);

		fmt.Printf("Request on /sdp has been handled\n")
	})

	// A HTTP handler that processes a get candidate request
	http.HandleFunc("/getcandidate", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Request on /getcandidate\n")

		candidatesMux.Lock()
		defer candidatesMux.Unlock()

		if len(pendingCandidates) > 0 {
			candidate := pendingCandidates[0]
			pendingCandidates = pendingCandidates[1:]

			payload, err := json.Marshal(candidate)
			if err != nil {
				panic(err)
			}
			
			w.Write(payload);
		}

		fmt.Printf("Request on /getcandidate has been handled\n")
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
			fmt.Println("Peer Connection has gone to failed exiting")
			os.Exit(0)
		}
	})

	// Register data channel creation handling
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

		// Register channel opening handling
		d.OnOpen(func() {
			fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", d.Label(), d.ID())

			for range time.NewTicker(1 * time.Second).C {
				message := signal.RandSeq(15)
				fmt.Printf("Sending '%s'\n", message)

				// Send the message as text
				sendTextErr := d.SendText(message)
				if sendTextErr != nil {
					panic(sendTextErr)
				}
			}
		})

		// Register text message handling
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
		})
	})

	fmt.Printf("Starting server\n")

	// Start HTTP server that accepts requests from the offer process to exchange SDP and Candidates
	panic(http.ListenAndServe(*answerAddr, nil))
}
