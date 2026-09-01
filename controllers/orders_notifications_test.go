package controllers

import "testing"

func TestStatusNotificationIsOnlyNeededForTransitions(t *testing.T) {
	if isStatusTransition("ready", "ready") {
		t.Fatalf("unchanged status was treated as a transition")
	}
	if !isStatusTransition("new", "ready") {
		t.Fatalf("changed status was not treated as a transition")
	}
}
