package rpc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// AdminUser mirrors the users service's admin wire format. Status is the raw
// DB enum: "free", "paid", or "vip". Tier and PremiumKind are derived by the
// UI layer from Status so the templ conditionals stay readable.
type AdminUser struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	IsActive  bool      `json:"is_active"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Tier returns "premium" when the user is paid or vip, else "standard".
func (u AdminUser) Tier() string {
	if u.Status == "paid" || u.Status == "vip" {
		return "premium"
	}
	return "standard"
}

// PremiumKind returns "vip", "paid", or "" to mirror the former field shape.
func (u AdminUser) PremiumKind() string {
	switch u.Status {
	case "vip":
		return "vip"
	case "paid":
		return "paid"
	}
	return ""
}

type UserStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

// TokenStatus reports whether a user has a stored Twitch token. The admin
// panel uses it to manage the bot account's own token without ever reading
// the token back.
type TokenStatus struct {
	Present bool `json:"present"`
}

type usersReply struct {
	User  *AdminUser   `json:"user"`
	Users []AdminUser  `json:"users"`
	Stats *UserStats   `json:"stats"`
	Token *TokenStatus `json:"token"`
	Error string       `json:"error"`
}

// Users speaks the bagel.rpc.admin.user.* request-reply verbs owned by
// broadcaster-data. The admin tool never opens the database.
type Users struct {
	nc     *nats.Conn
	prefix string
}

func NewUsers(nc *nats.Conn, prefix string) *Users {
	return &Users{nc: nc, prefix: prefix}
}

func (u *Users) call(verb string, req map[string]any) (usersReply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return usersReply{}, err
	}
	msg, err := u.nc.Request(u.prefix+"."+verb, body, 5*time.Second)
	if err != nil {
		return usersReply{}, err
	}
	var reply usersReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return usersReply{}, err
	}
	if reply.Error != "" {
		return usersReply{}, fmt.Errorf("%s", reply.Error)
	}
	return reply, nil
}

func one(reply usersReply, err error) (*AdminUser, error) {
	if err != nil {
		return nil, err
	}
	if reply.User == nil {
		return nil, fmt.Errorf("empty reply")
	}
	return reply.User, nil
}

// Lookup resolves a query that is either a numeric Twitch user id or a
// username.
func (u *Users) Lookup(q string) (*AdminUser, error) {
	req := map[string]any{"username": q}
	if isDigits(q) {
		req = map[string]any{"user_id": q}
	}
	return one(u.call("get", req))
}

// Recent lists the most recently updated users.
func (u *Users) Recent(limit int) ([]AdminUser, error) {
	reply, err := u.call("list", map[string]any{"limit": limit})
	if err != nil {
		return nil, err
	}
	return reply.Users, nil
}

func (u *Users) Stats() (*UserStats, error) {
	reply, err := u.call("stats", map[string]any{})
	if err != nil {
		return nil, err
	}
	if reply.Stats == nil {
		return nil, fmt.Errorf("empty stats reply")
	}
	return reply.Stats, nil
}

// SetStatus moves a user between vip, paid and standard.
func (u *Users) SetStatus(userID, status string) (*AdminUser, error) {
	return one(u.call("set_status", map[string]any{"user_id": userID, "status": status}))
}

// Reset clears the user's settings and timers, keeping account and tier.
func (u *Users) Reset(userID string) (*AdminUser, error) {
	return one(u.call("reset", map[string]any{"user_id": userID}))
}

func token(reply usersReply, err error) (*TokenStatus, error) {
	if err != nil {
		return nil, err
	}
	if reply.Token == nil {
		return nil, fmt.Errorf("empty token reply")
	}
	return reply.Token, nil
}

// TokenGet reports whether the user has a stored Twitch token.
func (u *Users) TokenGet(userID string) (*TokenStatus, error) {
	return token(u.call("token_status", map[string]any{"user_id": userID}))
}

// TokenSet stores (or replaces) the user's Twitch token; provisioning the
// row if the id has never been seen, so the bot account can be set up before
// it ever onboards.
func (u *Users) TokenSet(userID, accessToken, refreshToken string) (*TokenStatus, error) {
	return token(u.call("token_set", map[string]any{
		"user_id":       userID,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	}))
}

// TokenClear deletes the user's stored Twitch token.
func (u *Users) TokenClear(userID string) (*TokenStatus, error) {
	return token(u.call("token_clear", map[string]any{"user_id": userID}))
}

// IsDigits reports whether s is a plain numeric id (Twitch user id shape).
func IsDigits(s string) bool { return isDigits(s) }

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
