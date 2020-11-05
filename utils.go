package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
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

const timestampMillisFormat = "2006-01-02 15:04:05.000"

func stdPrettyPrint(v interface{}) []byte {
	s, err := json.MarshalIndent(v, "", "   ")
	if err != nil {
		fmt.Printf("%s [ERROR]: Failed to marshal struct. Error=%s\nmsg=%+v\n",
			time.Now().Format(timestampMillisFormat), err.Error(), v)
	}

	return s
}

func coloredPrettyPrint(v interface{}) []byte {
	s, err := prettyjson.Marshal(v)
	if err != nil {
		fmt.Printf("%s [ERROR]: Failed to marshal struct. Error=%s\nmsg=%+v\n",
			time.Now().Format(timestampMillisFormat), err.Error(), v)
	}

	return s
}

func tryUnmarshalJSONAsPushMessage(jsonMsg []byte, printStruct bool) (PushMessage, error) {
	var msg PushMessage
	err := json.Unmarshal(jsonMsg, &msg)
	if err != nil {
		e := fmt.Errorf("Error when unmarshalling incoming json.\nError=%s\nJSON:%d",
			err.Error(), jsonMsg)
		return PushMessage{}, e
	}

	return msg, nil
}

func printJsonWithTag(tag string, msg []byte) {
	var createdAt int64
	var s []byte
	var v interface{}
	var o map[string]interface{}
	var a []map[string]interface{}

	if bytes.HasPrefix(msg, []byte("[")) {
		err := json.Unmarshal(msg, &a)
		if err != nil {
			fmt.Printf("%s [ERROR]: Failed to unmarshal message. Error=%s\nmsg=%+v\n",
				time.Now().Format(timestampMillisFormat), err.Error(), a)
		}

		v = a
	} else {
		err := json.Unmarshal(msg, &o)
		if err != nil {
			fmt.Printf("%s [ERROR]: Failed to unmarshal message. Error=%s\nmsg=%+v\n",
				time.Now().Format(timestampMillisFormat), err.Error(), o)
		}

		if ts, ok := o["created_timestamp"]; ok {
			createdAt = int64(ts.(float64))
		}

		v = o
	}

	if *noPPFlag {
		s = stdPrettyPrint(v)
	} else {
		s = coloredPrettyPrint(v)
	}

	if createdAt != 0 {
		latency := roundDuration(time.Since(millisToTime(createdAt)), time.Millisecond)
		fmt.Printf("%s [%s] (latency: %s; %d bytes w/o pretty print):\n%s\n\n",
			time.Now().Format(timestampMillisFormat), tag, latency, len(msg), string(s))
	} else {
		fmt.Printf("%s [%s] (%d bytes w/o pretty print):\n%s\n\n",
			time.Now().Format(timestampMillisFormat), tag, len(msg), string(s))
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
		deleteSubscription(secret, subscriptionIDOrName)
		disconnectWebsocket()
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

func millisToTime(millis int64) time.Time {
	return time.Unix(0, millis*int64(time.Millisecond))
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
