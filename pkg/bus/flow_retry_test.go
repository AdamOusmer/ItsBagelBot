package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestRetryScheduleMatchesTheServerScheduleContract(t *testing.T) {
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	wire.Header.Set("Nats-Expected-Last-Sequence", "9")
	wire.Header.Set("traceparent", "00-trace-span-01")
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	schedule := retryScheduleMsg("twitch.ingress.event.premium", wire, 3*time.Second, now)

	for _, header := range []struct {
		name    string
		want    string
		failure string
	}{
		{jsapi.ScheduleHeader, "@at 2026-07-27T12:00:03Z", "schedule pattern must be a one-shot @at three seconds out"},
		{jsapi.ScheduleTargetHeader, "twitch.ingress.retry.premium", "schedule target"},
		// The server rejects @at outright when a time zone header is present, even UTC.
		{jsapi.ScheduleTimeZoneHeader, "", "schedule carried a time zone"},
		{jsapi.ScheduleTTLHeader, retryScheduleTTL, "schedule TTL"},
		{RetryCountHeader, "1", "retry count must be the first hop"},
		// Application headers survive the emit; Nats-* headers do not, and the
		// expectation headers among them would be re-evaluated against the retry
		// stream and reject the publish.
		{"traceparent", "00-trace-span-01", "application headers were dropped"},
		{MessageIDHeader, "logical-id", "fleet identity was dropped"},
		{"Nats-Expected-Last-Sequence", "", "an expectation header rode the retry"},
	} {
		requireScheduleHeader(t, schedule, header.name, header.want, header.failure)
	}
	if string(schedule.Data) != `{"text":"hello"}` {
		t.Fatalf("retry payload = %q", schedule.Data)
	}
}

// requireScheduleHeader states one term of the schedule contract: an empty want
// is the requirement that the header never rode the retry at all.
func requireScheduleHeader(t *testing.T, schedule *nats.Msg, name, want, failure string) {
	t.Helper()
	if got := schedule.Header.Get(name); got != want {
		t.Fatalf("%s: %s = %q, want %q (headers: %#v)", failure, name, got, want, schedule.Header)
	}
}

func TestRetryStampsIdentityWhenIngressWireHasNone(t *testing.T) {
	// Ingress-origin events reach the lane with no Bagelbot-Message-Id, so the
	// re-emitted retry would otherwise land under a fresh stream sequence and read
	// as a different message. The retry must stamp the delivery's resolved
	// identity so a consumer's dedup guard recognises it as the same event.
	wire := nats.NewMsg("twitch.ingress.event.standard")
	wire.Data = []byte(`{"text":"hello"}`)
	if wire.Header.Get(MessageIDHeader) != "" {
		t.Fatal("precondition: an ingress wire carries no message id")
	}

	schedule := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())
	if schedule.Header.Get(MessageIDHeader) == "" {
		t.Fatalf("retry left the fleet identity empty: %#v", schedule.Header)
	}
}

func TestRetrySchedulesNeverShareASubject(t *testing.T) {
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	first := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())
	second := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())

	// Publishing a schedule rolls its subject up, so a shared subject would purge
	// the previous retry before it ever fired.
	if first.Subject == second.Subject {
		t.Fatalf("two retries shared the schedule subject %q", first.Subject)
	}
	for _, subject := range []string{first.Subject, second.Subject} {
		if !strings.HasPrefix(subject, "twitch.ingress.retry.standard.") {
			t.Fatalf("schedule subject %q left the retry lane's namespace", subject)
		}
	}
}

func TestRetryLaneSubjectIsTheScheduleTarget(t *testing.T) {
	if got := RetryLaneSubject("twitch.ingress.event.premium"); got != "twitch.ingress.retry.premium" {
		t.Fatalf("retry lane = %q", got)
	}
}

func TestRetryDelayIsTunable(t *testing.T) {
	t.Setenv("NATS_FLOW_RETRY_DELAY", "")
	if got := flowRetryDelay(); got != defaultFlowRetryDelay {
		t.Fatalf("default retry delay = %v", got)
	}
	t.Setenv("NATS_FLOW_RETRY_DELAY", "12s")
	if got := flowRetryDelay(); got != 12*time.Second {
		t.Fatalf("configured retry delay = %v", got)
	}
	t.Setenv("NATS_FLOW_RETRY_DELAY", "-1s")
	if got := flowRetryDelay(); got != defaultFlowRetryDelay {
		t.Fatalf("negative retry delay = %v, want the default", got)
	}
}

func TestRejectedRetryScheduleIsAnError(t *testing.T) {
	rejected := nats.NewMsg("reply")
	rejected.Data = []byte(`{"error":{"code":400,"err_code":10188,"description":"message schedules is disabled"}}`)
	err := pubAckError(rejected)
	if err == nil || !strings.Contains(err.Error(), "10188") {
		t.Fatalf("rejected schedule error = %v", err)
	}

	accepted := nats.NewMsg("reply")
	accepted.Data = []byte(`{"stream":"TWITCH_INGRESS_RETRY","seq":12}`)
	if err := pubAckError(accepted); err != nil {
		t.Fatalf("accepted schedule reported %v", err)
	}
	if pubAckError(nil) == nil {
		t.Fatal("a missing acknowledgement was accepted")
	}
}

func TestAMarkedRetryIsDroppedWithACounter(t *testing.T) {
	sub := testFlowSubscriber()
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	wire.Header.Set(RetryCountHeader, "1")
	msg := mustFlowMessage(t, wire)

	// The budget check runs before anything touches the connection.
	sub.scheduleRetry(flowDelivery{wire: wire, msg: msg})
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want one drop", sub.dropped.Load(), sub.retried.Load())
	}
}
