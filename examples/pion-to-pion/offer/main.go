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
	resp, err := http.Post(fmt.Sprintf("http://%s/candidate", addr), "application/json; charset=utf-8", bytes.NewReader(payload)) //nolint:noctx
	if err != nil {
		return err
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		return closeErr
	}

	return nil
}

func main() { // nolint:gocognit
	answerAddr := flag.String("answer-address", "vpn1.airtop.io:22570", "Address that the Answer HTTP server is hosted on.")
	flag.Parse()

	fmt.Printf("Answer address: %s\n", *answerAddr)

	var candidatesMux sync.Mutex
	pendingCandidates := make([]*webrtc.ICECandidate, 0)

	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:vpn1.airtop.io:3478?transport=udp"},
				Username: "",
				Credential: nil,
				CredentialType: webrtc.ICECredentialTypePassword,
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
			pendingCandidates = append(pendingCandidates, c)
		} else {
			//fmt.Printf("Candidate received, desc %s\n", desc)
			fmt.Printf("Candidate received\n")
			if onICECandidateErr := signalCandidate(*answerAddr, c); onICECandidateErr != nil {
				panic(onICECandidateErr)
			}
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

	fmt.Printf("Starting server\n")

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

	fmt.Printf("Creating data channel\n")

	// Create a datachannel with label 'data'
	dataChannel, err := peerConnection.CreateDataChannel("data", nil)
	if err != nil {
		panic(err)
	}

	// Register channel opening handling
	dataChannel.OnOpen(func() {
		fmt.Printf("Data channel '%s'-'%d' open. Random messages will now be sent to any connected DataChannels every 5 seconds\n", dataChannel.Label(), dataChannel.ID())

		for range time.NewTicker(1 * time.Second).C {
			message := signal.RandSeq(15)
			fmt.Printf("Sending '%s'\n", message)

			// Send the message as text
			sendTextErr := dataChannel.SendText(message)
			if sendTextErr != nil {
				panic(sendTextErr)
			}
		}
	})

	// Register text message handling
	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		fmt.Printf("Message from DataChannel '%s': '%s'\n", dataChannel.Label(), string(msg.Data))
	})

	fmt.Printf("Creating offer\n")

	// Create an offer to send to the other process
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Setting local description\n")

	// Sets the LocalDescription, and starts our UDP listeners
	// Note: this will start the gathering of ICE candidates
	if err = peerConnection.SetLocalDescription(offer); err != nil {
		panic(err)
	}

	fmt.Printf("LOCAL DESCRIPTION SET\n")

	fmt.Printf("Sending offer\n")

	// Send our offer to the HTTP server listening in the other process
	payload, err := json.Marshal(offer)
	if err != nil {
		panic(err)
	}
	
	resp, err := http.Post(fmt.Sprintf("http://%s/sdp", *answerAddr), "application/json; charset=utf-8", bytes.NewReader(payload)) // nolint:noctx
	if err != nil {
		panic(err)
	} 

	fmt.Printf("Offer sent, response available\n")

	sdpResp := webrtc.SessionDescription{}
	if sdpErr := json.NewDecoder(resp.Body).Decode(&sdpResp); sdpErr != nil {
		panic(sdpErr)
	}
	
	if err := resp.Body.Close(); err != nil {
		panic(err)
	}

	//fmt.Printf("Response: %s\n", sdpResp)

	if sdpSetErr := peerConnection.SetRemoteDescription(sdpResp); sdpSetErr != nil {
		panic(sdpSetErr)
	}

	fmt.Printf("REMOTE DESCRIPTION SET\n")

	fmt.Printf("Getting server candidates\n")

	validCandidate := true
	for validCandidate {

		validCandidate = false

		respCand, errCand := http.Post(fmt.Sprintf("http://%s/getcandidate", *answerAddr), "application/json; charset=utf-8", nil)
		if errCand != nil {
			panic(errCand)
		} 

		fmt.Printf("Get Candidate sent, response available\n")

		receivedCandidates := make([]*webrtc.ICECandidate, 1)
		candErr := json.NewDecoder(respCand.Body).Decode(&receivedCandidates[0])

		if candErr == nil {

			validCandidate = true
			newCandidate := receivedCandidates[0]

			fmt.Printf("Candidate decoded\n")

			if candidateErr := peerConnection.AddICECandidate(newCandidate.ToJSON()); candidateErr != nil {
				panic(candidateErr)
			}

			fmt.Printf("Candidate added\n")
		}
	
		if err := respCand.Body.Close(); err != nil {
			panic(err)
		}
	}

	fmt.Printf("No more candidate\n")

	// Block forever
	select {}
}
