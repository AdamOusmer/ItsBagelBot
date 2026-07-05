//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
)

type raidEvent struct {
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	ToBroadcasterUserID      string `json:"to_broadcaster_user_id"`
	Viewers                  int    `json:"viewers"`
}

func main() {
	payload := `{"from_broadcaster_user_id":"1234","from_broadcaster_user_login":"jtv","from_broadcaster_user_name":"JTV","to_broadcaster_user_id":"5678","to_broadcaster_user_login":"twitch","to_broadcaster_user_name":"Twitch","viewers":9000}`
	var ev raidEvent
	err := json.Unmarshal([]byte(payload), &ev)
	fmt.Printf("%+v, err: %v\n", ev, err)
}
