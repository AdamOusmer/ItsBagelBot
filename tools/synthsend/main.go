// Command synthsend is a synthetic end-to-end latency probe for the outgress
// path. It publishes real chat sends onto the outgress premium lane (a real
// message lands in the target channel) and measures two stages:
//
//   - publish -> JetStream ack: how long NATS takes to accept the command.
//   - publish -> echo: the full round trip. The bot's own message comes back
//     as a channel.chat.message EventSub on the ingress event stream, so
//     subscribing to twitch.ingress.event.> and matching a per-send nonce
//     times the whole pipeline (hub -> leaf -> outgress -> Twitch POST ->
//     Twitch fanout -> EventSub -> ingress).
//
// The per-node split of the Twitch POST leg itself lives in New Relic (the
// outgress external segment is tagged node.region/node.name). This tool finds
// whether the bottleneck is NATS ingest, our pipeline, or Twitch-side, and
// generates the labeled traffic NR needs.
//
// Usage:
//
//	NATS_URL='nats://user:pass@127.0.0.1:4222' go run ./tools/synthsend \
//	  -id 804932984 -n 8 -pace 2s
//
// Point NATS_URL at a port-forward to the hub:
//
//	kubectl -n production port-forward svc/nats 4222:4222
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/outgress"

	"github.com/nats-io/nats.go"
)

func main() {
	var (
		broadcasterID = flag.String("id", "804932984", "target broadcaster id (default itsmavey)")
		count         = flag.Int("n", 8, "number of probe messages to send")
		pace          = flag.Duration("pace", 2*time.Second, "delay between sends (keep under the 20/30s chat limit)")
		subject       = flag.String("subject", "twitch.outgress.premium", "outgress lane subject")
		ingressSubj   = flag.String("ingress", "twitch.ingress.event.premium,twitch.ingress.event.standard", "comma-separated ingress subjects to watch for the echo (best-effort; worker_bus cannot use the wildcard)")
		domain        = flag.String("domain", "hub", "JetStream domain")
		echoWait      = flag.Duration("wait", 12*time.Second, "how long to wait for echoes after the last send")
		prefix        = flag.String("msg", "ItsBagelBot latency probe", "message text prefix")
	)
	flag.Parse()

	url := os.Getenv("NATS_URL")
	if url == "" {
		log.Fatal("NATS_URL is required (point it at a port-forward to the hub, with creds)")
	}

	// The hub advertises its in-cluster client URLs (10.42.x). Behind a
	// port-forward those are unreachable, so pin the client to the given URL.
	opts := []nats.Option{
		nats.Name("synthsend"), nats.Timeout(10 * time.Second),
		nats.IgnoreDiscoveredServers(), nats.DontRandomize(),
	}
	// The BUS account authenticates with NATS_USER/NATS_PASSWORD (e.g. worker_bus
	// from the worker Doppler project), matching pkg/bus. Inject via
	// `doppler run -- ...` so the password is never printed.
	if u := os.Getenv("NATS_USER"); u != "" {
		opts = append(opts, nats.UserInfo(u, os.Getenv("NATS_PASSWORD")))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream(nats.Domain(*domain))
	if err != nil {
		log.Fatalf("jetstream: %v", err)
	}

	runID := time.Now().Format("150405")

	type probe struct {
		nonce   string
		sentAt  time.Time
		ackDur  time.Duration
		echoAt  time.Time
		gotEcho bool
		ackErr  error
	}
	probes := make([]*probe, *count)
	for i := range probes {
		probes[i] = &probe{nonce: fmt.Sprintf("bagelprobe-%s-%d", runID, i)}
	}

	var mu sync.Mutex

	// Watch the ingress event stream for our nonce coming back. JetStream
	// publishes are ordinary core messages on the subject, so a plain
	// subscription sees them live.
	echoHandler := func(m *nats.Msg) {
		now := time.Now()
		mu.Lock()
		defer mu.Unlock()
		for _, p := range probes {
			if p.gotEcho {
				continue
			}
			// Substring match on the raw payload avoids depending on the exact
			// ingress event schema.
			if containsToken(m.Data, p.nonce) {
				p.echoAt = now
				p.gotEcho = true
			}
		}
	}
	for _, subj := range strings.Split(*ingressSubj, ",") {
		subj = strings.TrimSpace(subj)
		if subj == "" {
			continue
		}
		sub, err := nc.Subscribe(subj, echoHandler)
		if err != nil {
			log.Fatalf("subscribe %s: %v", subj, err)
		}
		defer func() { _ = sub.Unsubscribe() }()
	}
	_ = nc.Flush()

	fmt.Printf("synthsend: target=%s subject=%s n=%d pace=%s\n", *broadcasterID, *subject, *count, *pace)

	for i, p := range probes {
		text := fmt.Sprintf("%s %s", *prefix, p.nonce)
		inner, _ := json.Marshal(struct {
			BroadcasterID string `json:"broadcaster_id"`
			Message       string `json:"message"`
		}{*broadcasterID, text})

		msg := outgress.Message{
			Type:          outgress.TypeChat,
			BroadcasterID: *broadcasterID,
			Payload:       json.RawMessage(inner),
		}
		data, _ := json.Marshal(msg)

		sentAt := time.Now()
		_, ackErr := js.Publish(*subject, data)
		ackDur := time.Since(sentAt)

		mu.Lock()
		p.sentAt, p.ackDur, p.ackErr = sentAt, ackDur, ackErr
		mu.Unlock()

		if ackErr != nil {
			fmt.Printf("  [%d] publish FAILED: %v\n", i, ackErr)
		} else {
			fmt.Printf("  [%d] published ack=%.1fms\n", i, ms(ackDur))
		}
		if i < len(probes)-1 {
			time.Sleep(*pace)
		}
	}

	fmt.Printf("waiting up to %s for echoes...\n", *echoWait)
	deadline := time.Now().Add(*echoWait)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := true
		for _, p := range probes {
			if p.ackErr == nil && !p.gotEcho {
				done = false
				break
			}
		}
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Report.
	mu.Lock()
	defer mu.Unlock()
	var acks, e2es []time.Duration
	echoed := 0
	fmt.Println("\nper-send:")
	for i, p := range probes {
		if p.ackErr != nil {
			fmt.Printf("  [%d] ack-error\n", i)
			continue
		}
		acks = append(acks, p.ackDur)
		if p.gotEcho {
			e2e := p.echoAt.Sub(p.sentAt)
			e2es = append(e2es, e2e)
			echoed++
			fmt.Printf("  [%d] ack=%6.1fms  e2e=%7.1fms\n", i, ms(p.ackDur), ms(e2e))
		} else {
			fmt.Printf("  [%d] ack=%6.1fms  e2e=  (no echo)\n", i, ms(p.ackDur))
		}
	}

	fmt.Printf("\nsummary: sent=%d echoed=%d\n", len(acks), echoed)
	if len(acks) > 0 {
		fmt.Printf("  publish->ack   (NATS ingest):   %s\n", stat(acks))
	}
	if len(e2es) > 0 {
		fmt.Printf("  publish->echo  (full round trip): %s\n", stat(e2es))
		fmt.Println("  note: e2e includes Twitch-side fanout + EventSub delivery (~hundreds of ms, not ours).")
		fmt.Println("  the per-node Twitch POST leg is in New Relic, faceted by node.name.")
	} else {
		fmt.Println("  no echoes seen: channel chat EventSub may be inactive, or the send was rejected.")
		fmt.Println("  check outgress pod logs for the Twitch response.")
	}
}

func containsToken(data []byte, token string) bool {
	t := []byte(token)
	for i := 0; i+len(t) <= len(data); i++ {
		if string(data[i:i+len(t)]) == token {
			return true
		}
	}
	return false
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func stat(ds []time.Duration) string {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	p := func(q float64) time.Duration {
		if len(s) == 0 {
			return 0
		}
		idx := int(q * float64(len(s)-1))
		return s[idx]
	}
	return fmt.Sprintf("min=%.1f p50=%.1f p95=%.1f max=%.1f ms (n=%d)",
		ms(s[0]), ms(p(0.5)), ms(p(0.95)), ms(s[len(s)-1]), len(s))
}
