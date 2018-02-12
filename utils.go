package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	prettyjson "github.com/hokaccha/go-prettyjson"
	uuid "github.com/satori/go.uuid"
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

		v = o
	}

	if *colorPP {
		s = coloredPrettyPrint(v)
	} else {
		s = stdPrettyPrint(v)
	}

	fmt.Printf("%s [%s] (%d bytes w/o pretty print):\n%s\n\n",
		time.Now().Format(timestampMillisFormat), tag, len(msg), string(s))
}

// Intercept 'ctrl-c' and remove the subscription before shutdown
func setupSubscriptionRemoval(accessToken string, subscriptionID uuid.UUID) {
	sigs := make(chan os.Signal, 1)

	// `signal.Notify` registers the given channel to
	// receive notifications of the specified signals.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// This goroutine executes a blocking receive for
	// signals.
	go func() {
		<-sigs
		deleteSubscription(accessToken, subscriptionID)
		os.Exit(0)
	}()
}

func doRequestAccessToken(clientID string, clientSecret string) (string, error) {
	URL := *apiURL + "/oauth/access_token"
	form := url.Values{}
	form.Add("client_id", clientID)
	form.Add("client_secret", clientSecret)
	form.Add("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", URL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("")
	}

	var g AuthResp
	json.Unmarshal(respBody, &g)

	return g.AccessToken, nil
}

func buildHTTPURLFromWSURL(wsURL string) string {
	u, _ := url.Parse(wsURL)
	var scheme = ""
	if u.Scheme == "wss" {
		scheme = "https"
	} else {
		scheme = "http"
	}

	u.Scheme = scheme

	return u.String()
}
