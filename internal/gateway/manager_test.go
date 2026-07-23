package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestManagerDefaultsStoppedAndReleasesPort(t *testing.T) {
	store := openTestStore(t)
	manager := NewManager(store)
	if manager.Status().Running {
		t.Fatal("new gateway manager is running")
	}
	if err := manager.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if !status.Running || status.Address == "" {
		t.Fatalf("running status = %+v", status)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.Status().Running {
		t.Fatal("gateway still reports running")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", status.Address, 100*time.Millisecond)
		if err != nil {
			break
		}
		_ = connection.Close()
		if time.Now().After(deadline) {
			t.Fatal("gateway still accepts connections after shutdown")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerAuthenticatedModels(t *testing.T) {
	store := openTestStore(t)
	_, secret, err := store.CreateAPIKey(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store)
	if err := manager.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	client := &http.Client{Timeout: 2 * time.Second}
	baseURL := "http://" + manager.Status().Address
	response, err := client.Get(baseURL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("models status = %d", response.StatusCode)
	}
	var document struct {
		Data []Model `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if len(document.Data) == 0 || document.Data[0].ID != "grok-4.5" {
		t.Fatalf("models = %+v", document.Data)
	}
}

func TestValidateListenAddress(t *testing.T) {
	for _, invalid := range []string{"", ":8000", "example.com:8000", "127.0.0.1:70000"} {
		if _, err := validateListenAddress(invalid); err == nil {
			t.Fatalf("expected %q to be invalid", invalid)
		}
	}
	for _, valid := range []string{"127.0.0.1:8000", "0.0.0.0:8000", "localhost:8000", "[::1]:8000"} {
		if _, err := validateListenAddress(valid); err != nil {
			t.Fatalf("expected %q to be valid: %v", valid, err)
		}
	}
}
