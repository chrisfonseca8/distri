package statusstream

import (
	"sync"
	"testing"
	"time"
)

func TestHubNotifiesMatchingSubscribersAndCleansUp(t *testing.T) {
	hub := NewHub(2)
	one, cancelOne, err := hub.Subscribe("order-1")
	if err != nil {
		t.Fatal(err)
	}
	two, cancelTwo, err := hub.Subscribe("order-2")
	if err != nil {
		t.Fatal(err)
	}
	defer cancelTwo()

	hub.Notify("order-1")
	select {
	case <-one:
	case <-time.After(time.Second):
		t.Fatal("matching subscriber was not notified")
	}
	select {
	case <-two:
		t.Fatal("subscriber for another order was notified")
	default:
	}

	// Repeated notifications coalesce in the one-element channel.
	hub.Notify("order-1")
	hub.Notify("order-1")
	if len(one) != 1 {
		t.Fatalf("notification buffer length = %d, want 1", len(one))
	}
	cancelOne()
	cancelOne()
	if got := hub.Subscribers(); got != 1 {
		t.Fatalf("subscribers = %d, want 1", got)
	}
}

func TestHubEnforcesSubscriberLimit(t *testing.T) {
	hub := NewHub(1)
	_, cancel, err := hub.Subscribe("order-1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, _, err := hub.Subscribe("order-2"); err != ErrSubscriberLimit {
		t.Fatalf("second subscription error = %v, want %v", err, ErrSubscriberLimit)
	}
}

func TestHubConcurrentSubscribeNotifyAndCancel(t *testing.T) {
	hub := NewHub(1_000)
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, cancel, err := hub.Subscribe("order-1")
			if err != nil {
				t.Errorf("subscribe: %v", err)
				return
			}
			hub.Notify("order-1")
			cancel()
		}()
	}
	wait.Wait()
	if got := hub.Subscribers(); got != 0 {
		t.Fatalf("subscribers after concurrent cleanup = %d, want 0", got)
	}
}
