package main

import (
	"time"

	"github.com/gofrs/uuid"
)

// Base for all messages published to end-consumers
type Message struct {
	Channel string    `json:"channel"`
	UUID    uuid.UUID `json:"uuid"`
}

type PushMessage struct {
	Message
	Created time.Time              `json:"created"`
	Payload map[string]interface{} `json:"payload"`
}

// Base for messages sent on the 'system' channel
type SystemMessage struct {
	Message
	Cmd string `json:"cmd"`
}

// The 'init' system message
type InitResponseMessage struct {
	SystemMessage
	SubscriberID   uuid.UUID    `json:"subscriber_id"`
	ReconnectToken uuid.UUID    `json:"reconnect_token"`
	Subscription   Subscription `json:"subscription"`
	Reconnected    bool         `json:"reconnected"`
}

type Subscription struct {
	ID          uuid.UUID            `json:"id"`                    // Read-only, can't be set by the client when creating a subscription
	Description string               `json:"description,omitempty"` // Optional description of the subscription
	Name        string               `json:"name,omitempty"`        // Optional when creating a subscription
	Filters     []SubscriptionFilter `json:"filters"`
}

type SubscriptionFilter struct {
	Channel  string `json:"channel,omitempty"`
	GameID   int    `json:"game_id,omitempty"`
	SeriesID int    `json:"series_id,omitempty"`
	MatchID  int    `json:"match_id,omitempty"`
}
