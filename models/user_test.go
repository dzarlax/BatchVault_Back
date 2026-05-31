package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserJSONDoesNotExposePassword(t *testing.T) {
	user := User{
		ID:           1,
		Username:     "alice",
		Password:     "$2a$10$secret-hash",
		TokenVersion: 3,
	}

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("failed to marshal user: %v", err)
	}

	body := string(data)
	for _, forbidden := range []string{"password", "secret-hash", "tokenVersion", "token_version"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("user JSON exposed %q in body: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "alice") {
		t.Fatalf("user JSON did not include username: %s", body)
	}
}
