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

	// Add the auth credentials to the ws connection setup request
	var h http.Header
	if *clientV3SecretFlag != "" {
		// Set the Abios secret as a header in the request
		h = make(http.Header)
		h["Abios-Secret"] = []string{*clientV3SecretFlag}
	} else {
		accessToken, err := requestAccessToken(*clientV2IDFlag, *clientV2SecretFlag)
		if err != nil {
			return nil, fmt.Errorf("Access token request failed. Error: %v", err)
		}

		URL = URL + "&access_token=" + accessToken
	}

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

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

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

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

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

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return uuid.Nil, false, err
	}

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
	j, err := json.Marshal(sub)
	if err != nil {
		return uuid.Nil, false, err
	}

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

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return uuid.Nil, false, err
	}

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

func createAuthenticatedRequest(method string, endpoint string, body io.Reader) (*http.Request, error) {
	url := buildHTTPURLFromWSURL(*addrFlag)
	url = url + endpoint

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if *clientV3SecretFlag != "" {
		err = addV3Auth(req)
	} else {
		// Assume v2 auth token is used
		err = addV2Auth(req)
	}

	return req, err
}

// Adds the required Atlas v3 API secret to the request
func addV3Auth(req *http.Request) error {
	// Set the Abios secret as a header in the request
	req.Header["Abios-Secret"] = []string{*clientV3SecretFlag}

	return nil
}

// Adds the required v2 API secret to the request
func addV2Auth(req *http.Request) error {
	// Create an access token from the client id and secret given on the command line
	accessToken, err := requestAccessToken(*clientV2IDFlag, *clientV2SecretFlag)
	if err != nil {
		return fmt.Errorf("Access token request failed. Error: %v", err)
	}

	q := req.URL.Query()
	q.Add("access_token", accessToken) // Add the access_token to the list of parameters
	req.URL.RawQuery = q.Encode()      // Encode and assign back to the original query

	return nil
}
