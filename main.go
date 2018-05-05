package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
	flag "github.com/spf13/pflag"
)

var addrFlag = flag.String("addr", "wss://ws.abiosgaming.com/v0", "ws server address")
var subscriptionFileFlag = flag.String("subscription-file", "", "A file containing the subscription specification")
var subscriptionIDFlag = flag.String("subscription-id", "", "The id of a subscription that has been registered previously")
var clientIDFlag = flag.String("client-id", "", "Use client id for creating the access token")
var clientSecretFlag = flag.String("client-secret", "", "Use client secret for creating the access token")
var reconnectTokenFlag = flag.String("reconnect-token", "", "Use token to reconnect to previous subscriber state")
var noPPFlag = flag.Bool("no-pp", false, "Disable colorized pretty-print of JSON data")
var apiURLFlag = flag.String("access-token-url", "https://api.abiosgaming.com/v2", "URL for the access token creation")

var subscriptionIDOrName string
var currReconnectToken uuid.UUID
var conn *websocket.Conn

func main() {
	flag.Parse()

	err := validateFlags()
	if err != nil {
		fmt.Printf("%s [ERROR]: %s\n", time.Now().Format(timestampMillisFormat),
			err.Error())
		os.Exit(1)
	}
	// Create an access token from the client id and secret given on the command line
	accessToken, err := requestAccessToken(*clientIDFlag, *clientSecretFlag)
	if err != nil {
		fmt.Printf("%s [ERROR]: Access token request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		os.Exit(2)
	}

	// Let's look at our configuration. The information is only printed
	// to the terminal for debugging purposes, not used in any other way
	config, err := fetchPushServiceConfig(accessToken)
	if err != nil {
		fmt.Printf("%s [ERROR]: Config request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		os.Exit(2)
	}
	printJsonWithTag("PUSH CONFIG", config)

	// Fetch all subscriptions currently registered with the push service
	// only printed for debugging purposes, not used in any other way
	subs, err := fetchSubscriptions(accessToken)
	if err != nil {
		fmt.Printf("%s [ERROR]: Subscriptions list request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		os.Exit(2)
	}
	printJsonWithTag("EXISTING SUBSCRIPTIONS", subs)

	// If a subscription spec file has been supplied it will be registered
	// with the push service. If the subscription has a name and that name
	// already has been registered the existing subscription is updated
	// with the content of the supplied file.
	subscriptionIDOrName = registerOrUpdateSubscription(accessToken)

	// For this test client we'll delete the subscription
	// when we exit
	setupSubscriptionRemoval(accessToken, subscriptionIDOrName)

	// Parse the reconnect token given on the command line
	// and initialize the global variable with it
	reconnectToken, _ := uuid.FromString(*reconnectTokenFlag)

	// Now we have an access token and a registered subscription id/name we want to
	// connect to, the websocket can be created.
	// This will connect and wait for the init message response from the server
	conn = setupPushServiceConnection(accessToken, reconnectToken, subscriptionIDOrName)
	if conn == nil {
		// Failed to connect
		os.Exit(4)
	}

	// Start a separate process that sends a keep-alive ping now and then.
	go keepAliveLoop()

	// We start the infinite read loop as a separate go routine to simplify
	// the reconnect logic.
	go messageReadLoop()

	// Infinite wait here, use ctrl-c to kill program
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func setupPushServiceConnection(accessToken string, reconnectToken uuid.UUID, subscriptionIDOrName string) *websocket.Conn {
	// Connect the websocket to start receiving events that match
	// the subscription filters we set up previously
	conn := websocketConnectLoop(accessToken, reconnectToken, subscriptionIDOrName)

	// Read the 'init' message from server and handle any websocket setup errors
	initMsg, err := readInitMessage(conn)
	if err != nil {
		os.Exit(1)
	}

	// The init message contains a reconnect token, store it in case we need
	// to reconnect later
	var m InitResponseMessage
	json.Unmarshal(initMsg, &m)
	currReconnectToken = m.ReconnectToken

	printJsonWithTag("INIT MSG", initMsg)

	return conn
}

func websocketConnectLoop(accessToken string, reconnectToken uuid.UUID, subscriptionIDOrName string) *websocket.Conn {
	var conn *websocket.Conn
	for {
		var err error
		conn, err = connectToWebsocket(*addrFlag, reconnectToken, accessToken, subscriptionIDOrName)
		if err != nil {
			switch v := err.(type) {
			case *WebsocketSetupHTTPError:
				if v.HttpStatus == http.StatusUnauthorized {
					fmt.Printf("%s [WARNING]: Access token was not valid, requesting new.\n",
						time.Now().Format(timestampMillisFormat))
					accessToken, err = requestAccessToken(*clientIDFlag, *clientSecretFlag)
					if err != nil {
						fmt.Printf("%s [ERROR]: Access token request failed. Error='%s'\n",
							time.Now().Format(timestampMillisFormat), err.Error())
						os.Exit(1)
					}
				} else if v.HttpStatus == http.StatusTooManyRequests {
					// Client has been rate-limited, wait a while before trying again
					time.Sleep(time.Second * 30)
				}
			default:
				// Couldn't connect, try again in a while
				time.Sleep(time.Second * 5)
			}
		} else {
			// Connected successfully
			break
		}
	}

	return conn
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

		fmt.Printf("%s [ERROR]: Server closed connection: %s\n",
			time.Now().Format(timestampMillisFormat), errMsg)
		return nil, err
	} else if err != nil {
		// Websocket read encountered some other error, we won't try to recover
		fmt.Printf("%s [ERROR]: Failed to read `init' message. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return nil, err
	}

	return message, nil
}

// This will read messages from the server and print them to stdout.
// If the websocket is closed it will automatically re-establish the
// connection using the reconnect token to ensure no messages were lost
// during the disconnect.
func messageReadLoop() {
	// From here on we will start receiving push events that match our
	// subscription filters
	for {
		_, message, err := conn.ReadMessage()

		// If the websocket is closed we need to reconnect
		if closeErr, ok := err.(*websocket.CloseError); ok {
			fmt.Printf("%s [INFO]: Websocket was closed, starting reconnect loop. Reason='%s'\n",
				time.Now().Format(timestampMillisFormat), closeErr.Error())

			// Make sure to generate a new access token as the original one may be too old
			accessToken, err := requestAccessToken(*clientIDFlag, *clientSecretFlag)
			if err != nil {
				fmt.Printf("%s [ERROR]: Access token request failed. Error='%s'\n",
					time.Now().Format(timestampMillisFormat), err.Error())
				os.Exit(2)
			}

			// Reassign the global variable 'conn' with the new websocket handle
			conn = setupPushServiceConnection(accessToken, currReconnectToken, subscriptionIDOrName)
			if conn == nil {
				// Failed to connect
				os.Exit(4)
			}

			// Continue the message read loop
			continue
		} else if err != nil {
			// Websocket read encountered some other error, we won't try to recover
			fmt.Printf("%s [ERROR]: Failed to read message. Error='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error())

			os.Exit(3)
		}

		// Sanity check that the JSON can be marshalled into the correct message
		// format
		_, err = tryUnmarshalJSONAsPushMessage(message, false)
		if err != nil {
			fmt.Printf("%s [ERROR]: Failed to unmarshal to message struct. Error='%s', Message='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error(), message)

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
				fmt.Printf("%s [ERROR]: Failed to send Ping message. Error='%s'\n",
					time.Now().Format(timestampMillisFormat), err.Error())
				continue
			}
		}
	}
}

func registerOrUpdateSubscription(accessToken string) string {
	var subscriptionIDOrName string
	var sub Subscription
	var err error
	if *subscriptionFileFlag != "" {
		// Read subscription specification from file
		sub, err = readSubscriptionSpec(*subscriptionFileFlag)
		if err != nil {
			fmt.Printf("%s [ERROR]: Could not read subscription spec from file. Error='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error())
			return ""
		}

		// Register the subscription specification with the push service
		subscriptionID, alreadyExists, err := registerSubscription(accessToken, sub)
		if err != nil {
			fmt.Printf("%s [ERROR]: Subscription registration request failed. Error='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error())
			return ""
		}

		if alreadyExists {
			fmt.Printf("%s [INFO]: A subscription with name '%s' already exists, updating it.\n",
				time.Now().Format(timestampMillisFormat), sub.Name)
			sub.ID = subscriptionID
			updateSubscription(accessToken, sub)
		} else {
			if sub.Name != "" {
				fmt.Printf("%s [INFO]: Registered the subscription with name '%s' (ID=%s).\n",
					time.Now().Format(timestampMillisFormat), sub.Name, subscriptionID)
			} else {
				fmt.Printf("%s [INFO]: Registered the subscription. ID=%s.\n",
					time.Now().Format(timestampMillisFormat), subscriptionID)
			}
		}

		subscriptionIDOrName = subscriptionID.String()
	} else if *subscriptionIDFlag != "" {
		subscriptionIDOrName = *subscriptionIDFlag
	}

	return subscriptionIDOrName
}
