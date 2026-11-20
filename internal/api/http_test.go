package api_test

import (
	"agent-env/internal/api"
	"agent-env/internal/provider/memory"
	"agent-env/internal/sandbox"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	handler := api.New(sandbox.Service{Provider: memory.New()})
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	const path = "/v1/sandboxes"
	request("POST", path, `{"template":"unknown"}`, 400)
	request("POST", path, `{"template":"linux"}{}`, 400)
	response := request("POST", path, `{"template":"linux"}`, 201)
	var created sandbox.Sandbox
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.ID == "" || created.Status != "simulated" {
		t.Fatalf("unexpected creation: %s", response.Body.String())
	}
	target := path + "/" + created.ID
	if response.Header().Get("Location") != target {
		t.Fatal("wrong Location")
	}
	fetched := request("GET", target, "", 200)
	if fetched.Body.String() != response.Body.String() {
		t.Fatal("read does not match created sandbox")
	}
	request("DELETE", target, "", 204)
	request("DELETE", target, "", 204)
	request("GET", target, "", 404)
}
