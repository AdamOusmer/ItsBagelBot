package rpc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// AdminUser mirrors broadcaster-data's admin view of a user. Tier is the
// lane tier (premium|standard); PremiumKind records how premium was obtained
// (vip = operator grant, permanent; paid = Tebex).
type AdminUser struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	IsActive    bool      `json:"is_active"`
	Tier        string    `json:"tier"`
	PremiumKind string    `json:"premium_kind"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UserStats struct {
	TotalUsers   int `json:"total_users"`
	ActiveUsers  int `json:"active_users"`
	PremiumUsers int `json:"premium_users"`
	VIPUsers     int `json:"vip_users"`
	PaidUsers    int `json:"paid_users"`
}

type usersReply struct {
	User  *AdminUser  `json:"user"`
	Users []AdminUser `json:"users"`
	Stats *UserStats  `json:"stats"`
	Error string      `json:"error"`
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
