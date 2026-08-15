package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

const (
	flowControlHeartbeat = time.Second
	flowMaxAckPending    = 20_000

	// flowProvisionTimeout bounds an ordinary create/update. The replacement path
	// gets its own, much longer budget: a delete that succeeds followed by a
	// create that times out leaves the stream with no consumer at all.
	flowProvisionTimeout = 5 * time.Second
	laneReplaceTimeout   = 20 * time.Second

	// flowInactiveThreshold garbage-collects this pod's own consumer once the pod
	// is gone. nats-server 2.14.3 arms a delete timer whenever a push consumer's
	// delivery subject loses interest and honours the threshold for durables as
	// well as ephemerals (consumer.go updateInactiveThreshold / deleteNotActive),
	// so a consumer named after a pod that never comes back deletes itself. It is
	// deliberately far longer than a reconnect: a pod that reattaches inside the
	// window keeps its consumer and its ack floor.
	flowInactiveThreshold = 5 * time.Minute
)

// laneBinding is the stream/subject/consumer triple every provisioning step
// carries as a unit. The three are same-typed and were passed positionally,
// where a transposed pair is a consumer filtered on the wrong subject or bound
// to the wrong stream — both of which provision cleanly and then deliver
// nothing.
//
// The consumer name is the one this pod owns on that stream, so a config built
// from a binding always carries it as its Name: ensureFlowConsumer looks the
// live consumer up by the binding and updates the config it was handed, and
// those two must name the same durable.
type laneBinding struct {
	stream   string
	subject  string
	consumer string
}

// flowConsumerConfig keeps the hot consumer R3 while removing per-message
// consumer consensus. NATS AckFlowControl does not replicate delivery state
// before each push; a flow response advances the replicated ACK floor for a
// whole window.
//
// This is deliberately receipt-level acknowledgement. A hard process/node loss
// after receipt but before handler completion can lose as many as
// flowMaxAckPending (20,000) deliveries for this consumer. Graceful shutdown
// drains them; replay-sensitive control lanes remain on explicit ACK instead.
//
// There is deliberately NO DeliverGroup. The server publishes both the
// flow-control request and the idle heartbeat to the delivery subject as
// ordinary account messages with no queue-group targeting, so exactly one
// arbitrary member would receive each and would answer for the whole group out
// of its own cursor — and the ack is cumulative, so that one member's answer
// would acknowledge work still in flight on every other member. The floor would
// have to be the minimum across members, which the protocol cannot express.
// Each pod therefore owns its own consumer, its own delivery subject and its own
// cursor, and consequently receives the whole lane rather than a share of it:
// handlers must be idempotent and must not assume the fleet splits a lane.
//
// DeliverNew applies only to a first-ever creation. The lane stream retains five
// minutes of firehose, so a consumer created at DeliverAll would open by
// replaying every retained chat event; ensureFlowConsumer preserves the
// creation-time position on update and resumes at the predecessor's ack floor on
// replacement, falling back to DeliverNew rather than to an unknown inherited
// policy when that floor is unavailable.
func flowConsumerConfig(lane laneBinding) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:           lane.consumer,
		Durable:        lane.consumer,
		Description:    "ItsBagelBot R3 flow-controlled ingress lane consumer",
		DeliverPolicy:  jsapi.DeliverNewPolicy,
		AckPolicy:      jsapi.AckFlowControlPolicy,
		MaxDeliver:     -1,
		FilterSubject:  lane.subject,
		ReplayPolicy:   jsapi.ReplayInstantPolicy,
		MaxAckPending:  flowMaxAckPending,
		DeliverSubject: "_INBOX.BAGEL." + subjectToken(lane.consumer),
		FlowControl:    true,
		// The server rejects any other heartbeat for this ack policy: the same
		// interval bounds the stalled-source timeout it shares with sourcing.
		IdleHeartbeat: flowControlHeartbeat,
		// One consumer per pod means one consumer per pod that dies. The server
		// deletes it once its delivery subject has had no interest for this long.
		InactiveThreshold: flowInactiveThreshold,
		// R1 consumer state on an R3 stream, per NATS maintainer guidance for
		// high-rate consumers: replicating per-consumer ack state costs the
		// stream leader RAFT work on the hot path and buys nothing this design
		// needs. The consumer is memory-backed and per-pod already; a leader
		// change or state loss just means this pod re-provisions and resumes
		// from its own receipt cursor, and the idempotency guard absorbs the
		// redelivered window. The stream's data durability is untouched.
		Replicas:      1,
		MemoryStorage: true,
		Metadata:      map[string]string{managedConsumerMetadata: "true"},
	}
}

// flowConsumerName is this pod's own durable. The group and subject keep the
// fleet-wide naming contract; the pod identity is what makes the consumer
// single-subscriber, which is the only shape AckFlowControl has coherent
// acknowledgement semantics for.
func flowConsumerName(group, subject string) string {
	return durableName(group, subject) + "_" + podIdentity()
}

// podIdentity resolves a token that is stable for the life of a pod and distinct
// between pods. A restarted pod under the same name deliberately reuses its
// consumer, which keeps the ack floor; anything else falls back to a random
// token that the inactive threshold cleans up.
func podIdentity() string {
	name := env.Get("POD_NAME", env.Get("HOSTNAME", ""))
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = nuid.Next()
	}
	return consumerToken(name)
}

// consumerToken reduces an arbitrary identity to the characters a JetStream
// consumer name accepts, and bounds its length so the durable stays inside the
// server's 255-character name limit.
func consumerToken(name string) string {
	token := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
			return char
		case char >= '0' && char <= '9', char == '-', char == '_':
			return char
		default:
			return '_'
		}
	}, name)
	if len(token) > 48 {
		token = token[:48]
	}
	return token
}

// recoveryFlowConsumerConfig rebuilds a consumer the watchdog found gone.
// Recovery resumes just past this process's receipt cursor rather than at the
// server's last-sent position, so nothing that was pushed but never arrived is
// skipped. An empty cursor keeps the creation default rather than guessing.
func recoveryFlowConsumerConfig(lane laneBinding, position flowPosition) jsapi.ConsumerConfig {
	desired := flowConsumerConfig(lane)
	if position.stream == 0 {
		return desired
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = position.stream + 1
	return desired
}

// immutableConsumerFieldErrors are the only 2.14 update rejections that justify
// destroying a live consumer. Everything else — a context deadline against a busy
// meta layer, no responders during an election, a lost race with another replica
// — must propagate unchanged: deleting on those turns a transient API failure
// into a lane-wide delivery reset for every pod bound to the stream.
//
// The two conversion errors are what a mode flip actually hits. Every lane
// consumer is named durableName(group, subject) whatever mode binds it, so
// switching NATS_CONSUME_MODE re-provisions the SAME durable with the other
// shape, and nats-server refuses that in place (checkNewConsumerConfig). Which
// message comes back depends only on the field order of that check: the flip
// this fleet performs today trips "ack policy" first (explicit push is
// AckExplicit, pull is AckAll), and the conversion messages surface whenever the
// ack policies happen to agree. Both are the same one-way door.
//
// Deliberately absent: "deliver policy"/"start sequence", which both provisioning
// paths echo from the server precisely so they cannot mismatch — a rejection
// there means the echo is broken, and destroying a consumer would hide the bug;
// "max waiting", which is left to fail loudly rather than earn a delete; and
// JSConsumerNameExist, which is not an immutable field at all but the server
// reporting that the old delivery subject still has a live subscriber.
var immutableConsumerFieldErrors = []string{
	"ack policy can not be updated",
	"flow control can not be updated",
	"heart beats can not be updated",
	"can not update push consumer to pull based",
	"can not update pull consumer to push based",
}

func requiresConsumerReplacement(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, immutable := range immutableConsumerFieldErrors {
		if strings.Contains(message, immutable) {
			return true
		}
	}
	return false
}

// ensureFlowConsumer provisions this pod's flow consumer and returns the
// delivery subject to bind, which is the server's value whenever one exists.
func ensureFlowConsumer(nc *nats.Conn, lane laneBinding, desired jsapi.ConsumerConfig) (string, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return "", fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), flowProvisionTimeout)
	defer cancel()

	info, err := flowConsumerInfo(ctx, js, lane)
	if err != nil {
		return "", err
	}
	if info == nil {
		_, err = js.CreateOrUpdatePushConsumer(ctx, lane.stream, desired)
		return desired.DeliverSubject, err
	}

	// Preserve the stable binding and the creation-time start position on an
	// ordinary update; both are immutable, so sending anything else guarantees a
	// rejection every boot.
	desired.DeliverSubject = info.Config.DeliverSubject
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	if _, err = js.UpdatePushConsumer(ctx, lane.stream, desired); err == nil {
		return desired.DeliverSubject, nil
	}
	if !requiresConsumerReplacement(err) {
		return "", fmt.Errorf("bus: update flow consumer %q: %w", desired.Name, err)
	}

	carryLaneAckFloor(&desired, info)
	return desired.DeliverSubject, replaceFlowConsumer(js, lane, desired, err)
}

func flowConsumerInfo(ctx context.Context, js jsapi.JetStream, lane laneBinding) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.PushConsumer(ctx, lane.stream, lane.consumer)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}

// replaceFlowConsumer performs the delete + recreate an immutable-field
// transition requires, on its own budget so the create half cannot inherit an
// already-spent deadline from the update that failed. The caller has rewritten
// desired's delivery position, so the recreation never replays handled messages.
func replaceFlowConsumer(
	js jsapi.JetStream,
	lane laneBinding,
	desired jsapi.ConsumerConfig,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), laneReplaceTimeout)
	defer cancel()

	if err := js.DeleteConsumer(ctx, lane.stream, desired.Name); err != nil &&
		!errors.Is(err, jsapi.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update flow consumer %q: %w (replace failed: %v)", desired.Name, cause, err)
	}
	if _, err := js.CreateOrUpdatePushConsumer(ctx, lane.stream, desired); err != nil {
		return fmt.Errorf("bus: recreate flow consumer %q: %w", desired.Name, err)
	}
	return nil
}

// carryLaneAckFloor pins the replacement's start position, for either
// receipt-level path: both replace the same explicit-ACK durable, so both
// inherit the same hazard. An unknown ack floor must never fall through to the
// predecessor's own DeliverPolicy: the explicit-ACK lane consumer this replaces
// is DeliverAll, so inheriting it opens the replacement on the whole retained
// firehose and re-executes every chat command in the window. DeliverNew loses the
// unacked tail instead, which is the bounded failure of the two.
func carryLaneAckFloor(desired *jsapi.ConsumerConfig, info *jsapi.ConsumerInfo) {
	if info == nil || info.AckFloor.Stream == 0 {
		desired.DeliverPolicy = jsapi.DeliverNewPolicy
		desired.OptStartSeq = 0
		return
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = info.AckFloor.Stream + 1
}
