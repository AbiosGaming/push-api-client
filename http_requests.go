package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	uuid "github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
)

type WebsocketSetupHTTPError struct {
	error
	HttpStatus int
}

var httpClient = &http.Client{
	Timeout: time.Second * 10,
}

func connectToWebsocket(wsURL string, reconnectToken uuid.UUID, subscriptionIDOrName string) (*websocket.Conn, error) {
	URL := wsURL + "?subscription_id=" + subscriptionIDOrName
	if reconnectToken != uuid.Nil {
		URL = URL + "&reconnect_token=" + reconnectToken.String()
	}

	// Set the Abios secret as a header in the request
	var h http.Header = make(http.Header)
	h["Abios-Secret"] = []string{*clientSecretFlag}

	var dialer *websocket.Dialer
	conn, resp, err := dialer.Dial(URL, h)

	if err != nil {
		if resp != nil {
			return nil, WebsocketSetupHTTPError{HttpStatus: resp.StatusCode, error: err}
		} else {
			return nil, err
		}
	}

	return conn, nil
}

func createAuthenticatedRequest(method string, endpoint string, body io.Reader) (*http.Request, error) {
	url := buildHTTPURLFromWSURL(*addrFlag)
	url = url + endpoint

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Set the Abios secret as a header in the request
	req.Header["Abios-Secret"] = []string{*clientSecretFlag}

	return req, nil
}

func fetchPushServiceConfig() ([]byte, error) {
	req, err := createAuthenticatedRequest(http.MethodGet, "/config", nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
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

func fetchSubscriptions() ([]byte, error) {
	req, err := createAuthenticatedRequest(http.MethodGet, "/subscription", nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
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

func registerSubscription(sub Subscription) (uuid.UUID, bool, error) {
	j, _ := json.Marshal(sub)

	req, err := createAuthenticatedRequest(http.MethodPost, "/subscription", bytes.NewBuffer(j))
	if err != nil {
		return uuid.Nil, false, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
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

func updateSubscription(sub Subscription) (uuid.UUID, bool, error) {
	endpoint := "/subscription/" + sub.ID.String()
	j, _ := json.Marshal(sub)

	req, err := createAuthenticatedRequest(http.MethodPut, endpoint, bytes.NewBuffer(j))
	if err != nil {
		return uuid.Nil, false, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
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

func deleteSubscription(subscriptionIDOrName string) error {
	endpoint := "/subscription/" + subscriptionIDOrName
	req, err := createAuthenticatedRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
