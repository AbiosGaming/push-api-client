package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
)

var addr = flag.String("addr", "wss://ws.abiosgaming.com/v0", "ws server address")
var accessToken = flag.String("access-token", "", "Use given access token instead of client id + secret")
var clientID = flag.String("client-id", "", "Use client id for creating the access token")
var clientSecret = flag.String("client-secret", "", "Use client secret for creating the access token")
var reconnectToken = flag.String("reconnect-token", "", "Use token to reconnect to previous subscriber state")
var colorPP = flag.Bool("color-pp", false, "colorized pretty-print of JSON data")
var apiURL = flag.String("access-token-url", "https://api.abiosgaming.com/v2", "URL for the access token creation")

func main() {
	flag.Parse()

	// Create an access token from the client id and secret given on the command line
	accessToken, err := requestAccessToken(*clientID, *clientSecret)
	if err != nil {
		fmt.Printf("%s [ERROR]: Access token request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return
	}

	// Let's look at our configuration
	config, err := fetchPushServiceConfig(accessToken)
	if err != nil {
		fmt.Printf("%s [ERROR]: Config request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return
	}
	printJsonWithTag("PUSH CONFIG", config)

	subs, err := fetchSubscriptions(accessToken)
	if err != nil {
		fmt.Printf("%s [ERROR]: Subscriptions list request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return
	}
	printJsonWithTag("EXISTING SUBSCRIPTIONS", subs)

	var sub Subscription
	if false {
		// Create a subscription specification with a name
		sub = createNamedSubscription("sample_subscription")
	} else {
		sub = createSubscription()
	}

	// Register the subscription specification with the push service
	subscriptionID, err := registerSubscription(accessToken, sub)
	if err != nil {
		fmt.Printf("%s [ERROR]: Subscription request failed. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return
	}

	// For this test client we'll delete the subscription
	// when we exit
	setupSubscriptionRemoval(accessToken, subscriptionID)

	// Connect the websocket to start receiving events that match
	// the subscription filters we set up previously
	conn, err := connectToWebsocket(*addr, *reconnectToken, accessToken, subscriptionID)
	if err != nil {
		return
	}

	// Start a separate process that sends a keep-alive ping now and then
	go pinger(conn)

	// The first message we receive from the push service is always the init
	// on the 'system' channel
	initMsg, err := readInitMessage(conn)
	if err != nil {
		fmt.Printf("%s [ERROR]: Failed to read `init' message. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return
	} else {
		printJsonWithTag("INIT MSG", initMsg)
	}

	// From here on we will start receiving push events that match our
	// subscription filters
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("%s [ERROR]: Failed to read message. Error='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error())
			return
		}

		// Sanity check that the JSON can be marshalled into the correct message
		// format
		_, err = tryUnmarshalJSONAsPushMessage(message, false)
		if err != nil {
			fmt.Printf("%s [ERROR]: Failed to unmarshal to message struct. Error='%s'\n",
				time.Now().Format(timestampMillisFormat), err.Error())
			continue
		}

		printJsonWithTag("MSG", message)
	}
}

func requestAccessToken(clientID string, clientSecret string) (string, error) {
	var at string
	if *accessToken != "" {
		// The access token was given as a command-line option, use it
		at = *accessToken
	} else {
		var err error
		at, err = doRequestAccessToken(clientID, clientSecret)
		if err != nil {
			return "", err
		}
	}

	return at, nil
}

func connectToWebsocket(wsURL string, reconnectToken string, accessToken string, subscriptionID uuid.UUID) (*websocket.Conn, error) {
	URL := wsURL + "?"
	URL = URL + "access_token=" + accessToken
	URL = URL + "&subscription_id=" + subscriptionID.String()
	if reconnectToken != "" {
		URL = URL + "&reconnect_token=" + reconnectToken
	}
	var dialer *websocket.Dialer
	conn, resp, err := dialer.Dial(URL, nil)
	if err == websocket.ErrBadHandshake {
		fmt.Printf("%s [ERROR]: Failed to connect to WS url. Handshake status='%d'\n",
			time.Now().Format(timestampMillisFormat), resp.StatusCode)
		return nil, err
	} else if err != nil {
		fmt.Printf("%s [ERROR]: Failed to connect to WS url. Error='%s'\n",
			time.Now().Format(timestampMillisFormat), err.Error())
		return nil, err
	}

	return conn, nil
}

func readInitMessage(conn *websocket.Conn) ([]byte, error) {
	_, message, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	return message, nil
}

func fetchPushServiceConfig(accessToken string) ([]byte, error) {
	URL := buildHTTPURLFromWSURL(*addr)
	URL = URL + "/config"
	URL = URL + "?access_token=" + accessToken

	resp, err := http.Get(URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	return respBody, err
}

func fetchSubscriptions(accessToken string) ([]byte, error) {
	URL := buildHTTPURLFromWSURL(*addr)
	URL = URL + "/subscription"
	URL = URL + "?access_token=" + accessToken

	resp, err := http.Get(URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	return respBody, err
}

func createNamedSubscription(name string) Subscription {
	var filters []SubscriptionFilter
	f0 := SubscriptionFilter{
		Channel: "match", // This will give us all events from the 'match' channel
	}
	f1 := SubscriptionFilter{
		Channel: "series", // This will give us all events from the 'series' channel
	}
	filters = append(filters, f0, f1)

	sub := Subscription{
		Name:    name, // Set the optional name of the subscription
		Filters: filters,
	}

	return sub
}

func createSubscription() Subscription {
	var filters []SubscriptionFilter
	f0 := SubscriptionFilter{
		Channel: "match", // This will give us all events from the 'match' channel
	}
	f1 := SubscriptionFilter{
		Channel: "series", // This will give us all events from the 'series' channel
	}
	filters = append(filters, f0, f1)

	sub := Subscription{
		Filters: filters,
	}

	return sub
}

func registerSubscription(accessToken string, sub Subscription) (uuid.UUID, error) {
	URL := buildHTTPURLFromWSURL(*addr)
	URL = URL + "/subscription"
	URL = URL + "?access_token=" + accessToken

	j, _ := json.Marshal(sub)

	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(j))
	if err != nil {
		return uuid.Nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return uuid.Nil, err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return uuid.Nil, fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	var s struct {
		ID uuid.UUID `json:"id"`
	}
	json.Unmarshal(respBody, &s)

	return s.ID, err
}

func deleteSubscription(accessToken string, subscriptionID uuid.UUID) error {
	URL := buildHTTPURLFromWSURL(*addr)
	URL = URL + "/subscription/" + subscriptionID.String()
	URL = URL + "?access_token=" + accessToken

	req, err := http.NewRequest("DELETE", URL, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	return nil

}

func pinger(conn *websocket.Conn) {
	for {
		time.Sleep(time.Second * 30)
		conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(3*time.Second))
	}
}
