package modulesrpc

// Quote is one saved channel quote as the quote verbs return it. Number is the
// channel-local id chat refers to (!quote 12); CreatedAt is the save date in
// RFC 3339 so the bot can append it to the readout.
type Quote struct {
	Number    uint64 `json:"number"`
	Text      string `json:"text"`
	AddedBy   string `json:"added_by,omitempty"`
	CreatedAt string `json:"created_at"`
}

// QuoteRequest covers every quote verb (bagel.rpc.modules.quote.*); unused
// fields are zero-valued.
type QuoteRequest struct {
	UserID    string `json:"user_id"`              // broadcaster Twitch id
	Number    uint64 `json:"number,omitempty"`     // get/remove target
	Text      string `json:"text,omitempty"`       // add body
	AddedBy   string `json:"added_by,omitempty"`   // login of the mod who saved it
	CreatedAt string `json:"created_at,omitempty"` // optional RFC 3339 date chosen by the dashboard
}

// QuoteReply is the reply shape for every quote verb. A missing quote is not
// an error: get/random/remove set Found=false so the caller can answer chat
// with "no such quote" instead of failing. Quotes carries the full book for
// the list verb (the dashboard management page).
type QuoteReply struct {
	Quote  *Quote  `json:"quote,omitempty"`
	Quotes []Quote `json:"quotes,omitempty"`
	Found  bool    `json:"found,omitempty"`
	Error  string  `json:"error,omitempty"`
}
