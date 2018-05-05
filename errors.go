package main

import "fmt"

type WebsocketSetupHTTPError struct {
	HttpStatus int
}

func (e *WebsocketSetupHTTPError) Error() string {
	return fmt.Sprintf("Server did not respond with expected HTTP status 101 during setup. Actual status=%d", e.HttpStatus)
}
