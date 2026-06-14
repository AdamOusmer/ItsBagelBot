package web

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"

	"itsbagelbot/admin/ui/components"
)

// The lane mutation endpoints. Like userAction these require the X-Admin-Request
// header (a custom header forces a CORS preflight this server never grants, so a
// malicious public site in an operator's browser cannot drive them cross-origin)
// plus CSRF. They are the only place the admin writes to the broker, and each is
// gated in the UI behind an htmx confirm dialog.

func (s *Server) adminGuard(c *fiber.Ctx) bool { return c.Get("X-Admin-Request") == "1" }

// invalidateLanes drops the telemetry cache so the next read reflects a mutation.
func (s *Server) invalidateLanes() {
	s.lanesMu.Lock()
	if s.lanesSampler != nil {
		s.lanesSampler.until = time.Time{}
	}
	s.lanesMu.Unlock()
}

// laneResult re-renders the lane fragment with a one-shot notice; the 2s poll
// then re-renders without it.
func (s *Server) laneResult(c *fiber.Ctx, notice string, ok bool) error {
	s.invalidateLanes()
	lanes, errMsg := s.lanes()
	return render(c, components.LanesResult(notice, ok, lanes, errMsg))
}

// laneAlias sets (or clears) a lane's admin-side display alias. Cosmetic only.
func (s *Server) laneAlias(c *fiber.Ctx) error {
	if !s.adminGuard(c) {
		return c.Status(fiber.StatusForbidden).SendString("missing admin header")
	}
	stream := c.FormValue("stream")
	consumer := c.FormValue("consumer")
	alias := strings.TrimSpace(c.FormValue("alias"))
	if stream == "" || consumer == "" {
		return s.laneResult(c, "rename failed: missing lane", false)
	}
	if len(alias) > 48 {
		alias = alias[:48]
	}
	js, err := s.NATS.JetStream()
	if err != nil {
		return s.laneResult(c, "rename failed: "+err.Error(), false)
	}
	if err := s.store(js).setAlias(stream, consumer, alias); err != nil {
		s.Log.Warn("lane alias failed", "consumer", consumer, "err", err)
		return s.laneResult(c, "rename failed: "+err.Error(), false)
	}
	s.Log.Info("lane alias set", "stream", stream, "consumer", consumer, "alias", alias)
	if alias == "" {
		return s.laneResult(c, "alias cleared", true)
	}
	return s.laneResult(c, "renamed to "+alias, true)
}

// laneDurable converts an ephemeral lane into a permanent durable consumer with
// the same subject filter. Nothing drains it, so it retains new messages until
// the stream's own retention reclaims them. Refused on lanes that are already
// durable.
func (s *Server) laneDurable(c *fiber.Ctx) error {
	if !s.adminGuard(c) {
		return c.Status(fiber.StatusForbidden).SendString("missing admin header")
	}
	stream := c.FormValue("stream")
	consumer := c.FormValue("consumer")
	js, err := s.NATS.JetStream()
	if err != nil {
		return s.laneResult(c, "make-permanent failed: "+err.Error(), false)
	}
	info, err := js.ConsumerInfo(stream, consumer)
	if err != nil {
		return s.laneResult(c, "make-permanent failed: "+err.Error(), false)
	}
	if info.Config.Durable != "" {
		return s.laneResult(c, "lane is already durable", false)
	}
	name := "adminperm_" + subjectToken(info.Config.FilterSubject)
	if _, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable:       name,
		Description:   "operator-pinned permanent lane (admin)",
		FilterSubject: info.Config.FilterSubject,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverPolicy: nats.DeliverNewPolicy,
	}); err != nil {
		s.Log.Warn("lane make-durable failed", "consumer", consumer, "err", err)
		return s.laneResult(c, "make-permanent failed: "+err.Error(), false)
	}
	s.Log.Info("lane made permanent", "stream", stream, "source", consumer, "durable", name)
	return s.laneResult(c, "created permanent lane "+name+" (nothing drains it; it retains until stream retention)", true)
}

// laneDelete removes a consumer with no bound subscriber ("no system attached").
// It refuses any lane whose deliver subject still has a live subscriber, so a
// briefly-restarting service is never deleted out from under itself.
func (s *Server) laneDelete(c *fiber.Ctx) error {
	if !s.adminGuard(c) {
		return c.Status(fiber.StatusForbidden).SendString("missing admin header")
	}
	stream := c.FormValue("stream")
	consumer := c.FormValue("consumer")
	js, err := s.NATS.JetStream()
	if err != nil {
		return s.laneResult(c, "delete failed: "+err.Error(), false)
	}
	info, err := js.ConsumerInfo(stream, consumer)
	if err != nil {
		return s.laneResult(c, "delete failed: "+err.Error(), false)
	}
	if info.PushBound {
		return s.laneResult(c, "refused: lane is bound to a running consumer, not an orphan", false)
	}
	if err := js.DeleteConsumer(stream, consumer); err != nil {
		s.Log.Warn("lane delete failed", "consumer", consumer, "err", err)
		return s.laneResult(c, "delete failed: "+err.Error(), false)
	}
	_ = s.store(js).setAlias(stream, consumer, "") // drop any stale alias
	s.Log.Info("lane deleted", "stream", stream, "consumer", consumer)
	return s.laneResult(c, "deleted orphan lane "+consumer, true)
}
