package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	prettyjson "github.com/hokaccha/go-prettyjson"
)

// Custom status codes sent by the server for the 'close' command.
// The websocket standard (RFC6455) allocates the
// 4000-4999 range to application specific status codes.
const (
	CloseMissingSecret         = 4000 // Missing access token in ws setup request
	CloseInvalidSecret         = 4001 // Invalid access token in ws setup request
	CloseNotAuthorized         = 4002 // Client account does not have access to the push API
	CloseMaxNumSubscribers     = 4003 // Max number of concurrent subscribers connected for client id
	CloseMaxNumSubscriptions   = 4004 // Max number of registered subscriptions exist for client id
	CloseInvalidReconnectToken = 4005 // Invalid reconnect token in ws setup request
	CloseMissingSubscriptionID = 4006 // Missing subscription id in ws setup request
	CloseUnknownSubscriptionID = 4007 // The supplied subscriber id in ws setup request does not exist in server
	CloseInternalError         = 4500 // Unspecified error due to problem in server
)

func stdPrettyPrint(v interface{}) ([]byte, error) {
	s, err := json.MarshalIndent(v, "", "   ")
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal struct. Error: %v, Msg: %v", err, v)
	}

	return s, nil
}

func coloredPrettyPrint(v interface{}) ([]byte, error) {
	s, err := prettyjson.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal struct. Error: %v, Msg: %v", err, v)
	}

	return s, nil
}

func tryUnmarshalJSONAsPushMessage(jsonMsg []byte, printStruct bool) (PushMessage, error) {
	var msg PushMessage
	err := json.Unmarshal(jsonMsg, &msg)
	if err != nil {
		e := fmt.Errorf("Error when unmarshalling incoming json. Error:%v, JSON:%s",
			err.Error(), string(jsonMsg))
		return PushMessage{}, e
	}

	return msg, nil
}

func printJsonWithTag(tag string, msg []byte) {
	var createdAt time.Time
	var s []byte
	var v interface{}
	var o map[string]interface{}
	var a []map[string]interface{}

	if bytes.HasPrefix(msg, []byte("[")) {
		err := json.Unmarshal(msg, &a)
		if err != nil {
			log.Printf("[ERROR] Failed to unmarshal message. Error: %s, Msg: %+v\n", err, a)
			return
		}

		v = a
	} else {
		err := json.Unmarshal(msg, &o)
		if err != nil {
			log.Printf("[ERROR] Failed to unmarshal message. Error: %s, Msg: %+v\n", err, o)
			return
		}

		if ts, ok := o["created"]; ok {
			s, ok := ts.(string)
			if ok {
				createdAt, _ = time.Parse(time.RFC3339, s)
			}
		}

		v = o
	}

	var err error
	if *noPPFlag {
		s, err = stdPrettyPrint(v)
	} else {
		s, err = coloredPrettyPrint(v)
	}
	if err != nil {
		log.Println("[ERROR] Failed to prettyprint message. Error:", s)
		return
	}

	if !createdAt.IsZero() {
		latency := roundDuration(time.Since(createdAt), time.Millisecond)
		log.Printf("[%s] (latency: %s; %d bytes w/o pretty print):\n%s\n\n", tag, latency, len(msg), string(s))
	} else {
		log.Printf("[%s] (%d bytes w/o pretty print):\n%s\n\n", tag, len(msg), string(s))
	}
}

// Intercept 'ctrl-c' and remove the subscription before shutdown
func setupSubscriptionRemoval(secret string, subscriptionIDOrName string) {
	sigs := make(chan os.Signal, 1)

	// `signal.Notify` registers the given channel to
	// receive notifications of the specified signals.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// This goroutine executes a blocking receive for
	// signals.
	go func() {
		<-sigs
		err := deleteSubscription(secret, subscriptionIDOrName)
		if err != nil {
			log.Println("[ERROR] Failed to delete subscription. Error: ", err)
		}
		err = disconnectWebsocket()
		if err != nil {
			log.Println("[ERROR] Failed to do clean websocket disconnect. Error: ", err)
		}

		// Exit with success code
		os.Exit(0)
	}()
}

func buildHTTPURLFromWSURL(wsURL string) string {
	u, _ := url.Parse(wsURL)
	var scheme string
	if u.Scheme == "wss" {
		scheme = "https"
	} else {
		scheme = "http"
	}

	u.Scheme = scheme

	return u.String()
}

func readSubscriptionSpec(fileName string) (Subscription, error) {
	b, err := ioutil.ReadFile(fileName)
	var sub Subscription
	if err != nil {
		return sub, err
	}

	err = json.Unmarshal(b, &sub)

	return sub, err
}

func validateFlags() error {
	if *clientSecretFlag == "" {
		return fmt.Errorf("You need to provide your secret key with '--secret'")
	}

	if *subscriptionFileFlag == "" && *subscriptionIDFlag == "" && *reconnectTokenFlag == "" {
		return fmt.Errorf("You need to provide one of the options '--subscription-file', '--subscription-id' or '--reconnect-token'")
	}

	return nil
}

// Taken from https://play.golang.org/p/QHocTHl8iR
func roundDuration(d, r time.Duration) time.Duration {
	if r <= 0 {
		return d
	}
	neg := d < 0
	if neg {
		d = -d
	}
	if m := d % r; m+m < r {
		d = d - m
	} else {
		d = d + r - m
	}
	if neg {
		return -d
	}
	return d
}
