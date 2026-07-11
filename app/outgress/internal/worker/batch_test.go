package worker

import (
	"errors"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
)

func TestBatchRetryNeverRepeatsClaimedItems(t *testing.T) {
	items := []outgress.Message{{Type: "one"}, {Type: "two"}, {Type: "three"}}
	next := 0
	var firstRun []string
	wantFailure := errors.New("ambiguous twitch failure")

	err := runBatchItems(items, next,
		func(value int) error { next = value; return nil },
		func(item outgress.Message) error {
			firstRun = append(firstRun, item.Type)
			if item.Type == "two" {
				return wantFailure
			}
			return nil
		},
	)
	if !errors.Is(err, wantFailure) {
		t.Fatalf("first run error = %v, want %v", err, wantFailure)
	}
	if next != 2 {
		t.Fatalf("checkpoint = %d, want 2", next)
	}
	if len(firstRun) != 2 || firstRun[0] != "one" || firstRun[1] != "two" {
		t.Fatalf("first run executed %v, want [one two] in order", firstRun)
	}

	var retry []string
	if err := runBatchItems(items, next,
		func(value int) error { next = value; return nil },
		func(item outgress.Message) error { retry = append(retry, item.Type); return nil },
	); err != nil {
		t.Fatal(err)
	}
	if len(retry) != 1 || retry[0] != "three" {
		t.Fatalf("retry executed %v, want only [three]", retry)
	}
}

func TestBatchCheckpointFailureDoesNotSendItem(t *testing.T) {
	want := errors.New("valkey unavailable")
	executed := false
	err := runBatchItems([]outgress.Message{{Type: "chat"}}, 0,
		func(int) error { return want },
		func(outgress.Message) error { executed = true; return nil },
	)
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v, want %v", err, want)
	}
	if executed {
		t.Fatal("item executed without an at-most-once checkpoint")
	}
}

func TestBatchWireCodecPreservesItems(t *testing.T) {
	body := []byte(`{"type":"batch","broadcaster_id":"123","payload":{"id":"batch-1","items":[{"type":"chat","payload":{"message":"one"}},{"type":"chat","payload":{"message":"two"}}]}}`)
	var got outgress.Message
	if err := decodeMessage(body, &got); err != nil {
		t.Fatal(err)
	}
	var batch outgress.Batch
	if err := decodeBatch(got.Payload, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.ID != "batch-1" || len(batch.Items) != 2 {
		t.Fatalf("decoded batch = %#v", batch)
	}
}

func TestBatchJSONDecoderPrecompiles(t *testing.T) {
	if err := PrepareJSON(); err != nil {
		t.Fatal(err)
	}
}
