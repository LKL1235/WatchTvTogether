package ably_test

import (
	"encoding/json"
	"testing"
	"time"

	ablyrealtime "watchtogether/internal/realtime/ably"
)

func TestRoomCapabilityGrantsOnlySubscribePresenceHistory(t *testing.T) {
	channel := ablyrealtime.ChannelName("watchtogether", "room-1")
	capability, err := ablyrealtime.RoomCapability(channel)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(capability), &decoded); err != nil {
		t.Fatal(err)
	}
	ops := decoded[channel]
	if len(decoded) != 1 || len(ops) != 3 {
		t.Fatalf("capability = %s", capability)
	}
	for _, forbidden := range []string{"publish", "*"} {
		for _, op := range ops {
			if op == forbidden {
				t.Fatalf("capability should not include %q: %s", forbidden, capability)
			}
		}
	}
}

func TestTokenTTLMilliseconds(t *testing.T) {
	if got := (30 * time.Minute).Milliseconds(); got != 1800000 {
		t.Fatalf("ttl milliseconds = %d", got)
	}
}
