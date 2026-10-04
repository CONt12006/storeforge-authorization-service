package session

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-Api-Key") != "secret" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"sessions":[{"id":"session-1","expires_at":%d}]}`, time.Now().Add(time.Hour).Unix())
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, "secret", time.Second)
	valid, err := client.Validate(context.Background(), 42, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("expected session to be valid")
	}
}
