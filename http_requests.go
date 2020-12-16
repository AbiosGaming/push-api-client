package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	uuid "github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
)

type WebsocketSetupHTTPError struct {
	error
	HttpStatus int
}

func connectToWebsocket(wsURL string, reconnectToken uuid.UUID, secret string, subscriptionIDOrName string) (*websocket.Conn, error) {
	URL := wsURL + "?"
	URL = URL + "secret=" + secret
	URL = URL + "&subscription_id=" + subscriptionIDOrName
	if reconnectToken != uuid.Nil {
		URL = URL + "&reconnect_token=" + reconnectToken.String()
	}
	var dialer *websocket.Dialer
	conn, resp, err := dialer.Dial(URL, nil)

	if err != nil {
		if resp != nil {
			return nil, WebsocketSetupHTTPError{HttpStatus: resp.StatusCode, error: err}
		} else {
			return nil, err
		}
	}

	return conn, nil
}

func fetchPushServiceConfig(secret string) ([]byte, error) {
	URL := buildHTTPURLFromWSURL(*addrFlag)
	URL = URL + "/config"
	URL = URL + "?secret=" + secret

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

func fetchSubscriptions(secret string) ([]byte, error) {
	URL := buildHTTPURLFromWSURL(*addrFlag)
	URL = URL + "/subscription"
	URL = URL + "?secret=" + secret

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

func registerSubscription(secret string, sub Subscription) (uuid.UUID, bool, error) {
	URL := buildHTTPURLFromWSURL(*addrFlag)
	URL = URL + "/subscription"
	URL = URL + "?secret=" + secret

	j, _ := json.Marshal(sub)

	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(j))
	if err != nil {
		return uuid.Nil, false, err
	}
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	// The subscription POST endpoint response have 2 normal status codes:
	//  * Unprocessable Entity (422)
	//    This is returned by the server if client tries to register a subscription
	//    with a name that has already been registered on the server.
	//  * OK (200)
	//    If the registration was successful
	if resp.StatusCode == http.StatusUnprocessableEntity {
		var existingID uuid.UUID

		// If we get HTTP response code 422 the server has also set
		// the 'Location' header with the ID of the existing subscription
		if resp.Header.Get("Location") != "" {
			existingID, err = uuid.FromString(resp.Header.Get("Location"))
			if err != nil {
				// Location header didn't contain a valid UUID
				return uuid.Nil, true, err
			}

			return existingID, true, nil
		}

		// Server didn't set a valid ID in the 'Location' header, this should never happen
		return uuid.Nil, true, fmt.Errorf("Subscription with name already exists, but failed to retrieve ID")
	} else if resp.StatusCode != http.StatusOK {
		return uuid.Nil, false, fmt.Errorf("Unexpected status code: %d. Response message: %s", resp.StatusCode, string(respBody))
	}

	var s struct {
		ID uuid.UUID `json:"id"`
	}
	err = json.Unmarshal(respBody, &s)

	return s.ID, false, err
}

func updateSubscription(secret string, sub Subscription) (uuid.UUID, bool, error) {
	URL := buildHTTPURLFromWSURL(*addrFlag)
	URL = URL + "/subscription/" + sub.ID.String()
	URL = URL + "?access_token=" + secret

	j, _ := json.Marshal(sub)

	req, err := http.NewRequest("PUT", URL, bytes.NewBuffer(j))
	if err != nil {
		return uuid.Nil, false, err
	}
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return uuid.Nil, true, nil
	} else if resp.StatusCode != http.StatusOK {
		return uuid.Nil, false, fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	var s struct {
		ID uuid.UUID `json:"id"`
	}
	err = json.Unmarshal(respBody, &s)

	return s.ID, false, err
}

func deleteSubscription(secret string, subscriptionIDOrName string) error {
	URL := buildHTTPURLFromWSURL(*addrFlag)
	URL = URL + "/subscription/" + subscriptionIDOrName
	URL = URL + "?secret=" + secret

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
