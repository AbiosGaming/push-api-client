package main

import uuid "github.com/satori/go.uuid"

type PushMessage struct {
	Channel          string                 `json:"channel"`
	UUID             uuid.UUID              `json:"uuid"`
	SendTimestamp    int64                  `json:"sent_timestamp"`
	CreatedTimestamp int64                  `json:"created_timestamp"`
	Payload          map[string]interface{} `json:"payload"`
}

type Subscription struct {
	ID      uuid.UUID            `json:"id"`             // Read-only, can't be set by the client when creating a subscription
	Name    string               `json:"name,omitempty"` // Optional when creating a subscription
	Filters []SubscriptionFilter `json:"filters"`
}

type SubscriptionFilter struct {
	Channel  string `json:"channel,omitempty"`
	GameID   int    `json:"game_id,omitempty"`
	SeriesID int    `json:"series_id,omitempty"`
	MatchID  int    `json:"match_id,omitempty"`
}

type AuthResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}
