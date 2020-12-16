package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
	flag "github.com/spf13/pflag"
)

var addrFlag = flag.String("addr", "wss://ws.abiosgaming.com/v0", "ws server address")
var subscriptionFileFlag = flag.String("subscription-file", "", "A file containing the subscription specification")
var subscriptionIDFlag = flag.String("subscription-id", "", "The id of a subscription that has been registered previously")
var clientSecretFlag = flag.String("secret", "", "The v3 authentication secret")
var reconnectTokenFlag = flag.String("reconnect-token", "", "Use token to reconnect to previous subscriber state")
var noPPFlag = flag.Bool("no-pp", false, "Disable colorized pretty-print of JSON data")

var subscriptionIDOrName string
var currReconnectToken uuid.UUID
var conn *websocket.Conn

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	flag.Parse()

	err := validateFlags()
	if err != nil {
		log.Fatalln("[ERROR]", err)
	}

	secret := *clientSecretFlag

	// Let's look at our configuration. The information is only printed
	// to the terminal for debugging purposes, not used in any other way
	config, err := fetchPushServiceConfig()
	if err != nil {
		log.Fatalln("[ERROR] Config request failed. Error: ", err)
	}
	printJsonWithTag("PUSH CONFIG", config)

	// Fetch all subscriptions currently registered with the push service
	// only printed for debugging purposes, not used in any other way
	subs, err := fetchSubscriptions()
	if err != nil {
		log.Fatalln("[ERROR] Subscriptions list request failed. Error: ", err)
	}

	printJsonWithTag("EXISTING SUBSCRIPTIONS", subs)

	// If a subscription spec file has been supplied it will be registered
	// with the push service. If the subscription has a name and that name
	// already has been registered the existing subscription is updated
	// with the content of the supplied file.
	var wasUpdated bool
	subscriptionIDOrName, wasUpdated, err = registerOrUpdateSubscription(secret)
	if err != nil {
		log.Fatalln("[ERROR] Failed to register or update subscription. Error: ", err)
	}

	// For this test client we'll delete the subscription
	// when we exit.
	// But make sure to NOT delete it if the subscription already existed.
	if !wasUpdated {
		setupSubscriptionRemoval(secret, subscriptionIDOrName)
	}

	// Parse the reconnect token given on the command line
	// and initialize the global variable with it
	reconnectToken, _ := uuid.FromString(*reconnectTokenFlag)

	// Now we have an access token and a registered subscription id/name we want to
	// connect to, the websocket can be created.
	// This will connect and wait for the init message response from the server
	conn, err = setupPushServiceConnection(secret, reconnectToken, subscriptionIDOrName)
	if err != nil {
		log.Fatalln("[ERROR] Failed to connect to push service. Error: ", err)
	}

	// Start a separate process that sends a keep-alive ping now and then.
	go keepAliveLoop()

	// We start the infinite read loop as a separate go routine to simplify
	// the reconnect logic.
	go messageReadLoop(secret)

	// Infinite wait here, use ctrl-c to kill program
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func setupPushServiceConnection(secret string, reconnectToken uuid.UUID, subscriptionIDOrName string) (*websocket.Conn, error) {
	// Connect the websocket to start receiving events that match
	// the subscription filters we set up previously
	conn, err := websocketConnectLoop(secret, reconnectToken, subscriptionIDOrName)
	if err != nil {
		return nil, err
	}

	// Read the 'init' message from server and handle any websocket setup errors
	initMsg, err := readInitMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("Failed to read initial message from server. Error: %v", err)
	}

	// The init message contains a reconnect token, store it in case we need
	// to reconnect later
	var m InitResponseMessage
	err = json.Unmarshal(initMsg, &m)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal init response. Error: %v", err)
	}
	currReconnectToken = m.ReconnectToken

	printJsonWithTag("INIT MSG", initMsg)

	return conn, nil
}

func websocketConnectLoop(secret string, reconnectToken uuid.UUID, subscriptionIDOrName string) (*websocket.Conn, error) {
	var conn *websocket.Conn
	for {
		var err error
		conn, err = connectToWebsocket(*addrFlag, reconnectToken, subscriptionIDOrName)
		if err != nil {
			switch v := err.(type) {
			case *WebsocketSetupHTTPError:
				if v.HttpStatus == http.StatusUnauthorized {
					return nil, fmt.Errorf("Failed to authorize client. Error: %v", err)
				} else if v.HttpStatus == http.StatusTooManyRequests {
					// Client has been rate-limited, wait a while before trying again
					backoffSeconds := 30
					log.Println(fmt.Sprintf("[WARN] Client is rate-limited, retrying in %d seconds. Error: ", backoffSeconds), err)
					time.Sleep(time.Second * time.Duration(backoffSeconds))
				}
			default:
				// Couldn't connect, try again in a while
				backoffSeconds := 5
				log.Println(fmt.Sprintf("[ERROR]: Couldn't connect, retrying in %d seconds. Error:", backoffSeconds), err)
				time.Sleep(time.Second * time.Duration(backoffSeconds))
			}
		} else {
			// Connected successfully
			break
		}
	}

	return conn, nil
}

func disconnectWebsocket() error {
	if conn != nil {
		err := conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(3*time.Second))
		if err != nil {
			return fmt.Errorf("Failed to send Close message. Error: %v", err)
		}
	}

	return nil
}

func readInitMessage(conn *websocket.Conn) ([]byte, error) {
	// The push api server will validate a number of things during websocket
	// setup, e.g. that the access token is valid, user is authorized etc.
	// If any validation fails, the server will close the websocket and set
	// a custom error code.
	_, message, err := conn.ReadMessage()
	if closeErr, ok := err.(*websocket.CloseError); ok {
		var errMsg string
		switch closeErr.Code {
		case CloseUnknownSubscriptionID:
			errMsg = fmt.Sprintf("Subscription ID '%s' is not registered on server", subscriptionIDOrName)
		case CloseMissingSubscriptionID:
			errMsg = "Missing subscription ID or name in setup request"
		case CloseMaxNumSubscribers:
			errMsg = "The max number of concurrent subscribers for the account has been exceeded"
		case CloseMaxNumSubscriptions:
			errMsg = "The max number of registered subscriptions for the account has been exceeded"
		case CloseInternalError:
			errMsg = "Unknown server error"
		default:
			errMsg = fmt.Sprintf("Server sent unrecognized error code %d", closeErr.Code)
		}

		return nil, fmt.Errorf("Server closed connection with message: %s", errMsg)
	} else if err != nil {
		return nil, err
	}

	return message, nil
}

// This will read messages from the server and print them to stdout.
// If the websocket is closed it will automatically re-establish the
// connection using the reconnect token to ensure no messages were lost
// during the disconnect.
func messageReadLoop(secret string) {
	// From here on we will start receiving push events that match our
	// subscription filters
	for {
		_, message, err := conn.ReadMessage()

		// If the websocket is closed we need to reconnect
		if closeErr, ok := err.(*websocket.CloseError); ok {
			log.Println("[INFO] Websocket was closed, starting reconnect loop. Reason: ", closeErr)

			// Reassign the global variable 'conn' with the new websocket handle
			conn, err = setupPushServiceConnection(secret, currReconnectToken, subscriptionIDOrName)
			if err != nil {
				log.Fatalln("[ERROR] Failed to connect to push service. Error: ", err)
			}

			// Continue the message read loop
			continue
		} else if err != nil {
			// Websocket read encountered some other error, we won't try to recover
			log.Fatalln("[ERROR] Failed to read message. Error: ", err)
		}

		// Sanity check that the JSON can be marshalled into the correct message
		// format
		_, err = tryUnmarshalJSONAsPushMessage(message, false)
		if err != nil {
			log.Printf("[ERROR] Failed to unmarshal incoming message to message struct. Error: '%s', Message: '%s'\n", err.Error(), message)

			// Ignore message and keep reading from websocket
			continue
		}

		printJsonWithTag("MSG", message)
	}
}

// The client needs to have a keep-alive loop for two reasons:
//  1. Since the client does not send any other messages to the server
//     it will never get a notification if the websocket is closed.
//     The client only detects a closed websocket when it tries to write
//     data to it. Sending a ping message ensures this happens.
//  2. The server (or other network devices on the route to the server)
//     will close connections that are idle for too long.
func keepAliveLoop() {
	for {
		time.Sleep(time.Second * 30)
		if conn != nil {
			err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(3*time.Second))
			if err != nil {
				log.Println("[ERROR] Failed to send Ping message. Error: ", err)
				continue
			}
		}
	}
}

func registerOrUpdateSubscription(secret string) (string, bool, error) {
	var subscriptionIDOrName string
	var sub Subscription
	var err error
	var alreadyExists bool
	if *subscriptionFileFlag != "" {
		// Read subscription specification from file
		sub, err = readSubscriptionSpec(*subscriptionFileFlag)
		if err != nil {
			return "", false, fmt.Errorf("Could not read subscription spec from file. Error=%v", err)
		}

		// Register the subscription specification with the push service
		var subscriptionID uuid.UUID
		subscriptionID, alreadyExists, err = registerSubscription(sub)
		if err != nil {
			return "", false, fmt.Errorf("Subscription registration request failed. Error: %v", err)
		}

		if alreadyExists {
			log.Printf("[INFO]: A subscription with name '%s' already exists, updating it.\n", sub.Name)

			sub.ID = subscriptionID
			_, _, err = updateSubscription(sub)
			if err != nil {
				return "", false, fmt.Errorf("Failed to update subscription. Error: %v", err)
			}
		} else {
			if sub.Name != "" {
				log.Printf("[INFO]: Registered the subscription with name '%s' (ID=%s).\n", sub.Name, subscriptionID)
			} else {
				log.Printf("[INFO]: Registered the subscription. ID=%s.\n", subscriptionID)
			}
		}

		subscriptionIDOrName = subscriptionID.String()
	} else if *subscriptionIDFlag != "" {
		subscriptionIDOrName = *subscriptionIDFlag
	}

	return subscriptionIDOrName, alreadyExists, nil
}
