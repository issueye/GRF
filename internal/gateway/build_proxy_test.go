package gateway

import "testing"

func TestBuildClientProxyCanBeUpdated(t *testing.T) {
	client := NewBuildClient()
	if err := client.SetProxy("http://127.0.0.1:7890"); err != nil {
		t.Fatal(err)
	}
	proxyURL, err := client.proxy(nil)
	if err != nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = %v, err = %v", proxyURL, err)
	}
	if err := client.SetProxy(""); err != nil {
		t.Fatal(err)
	}
	proxyURL, err = client.proxy(nil)
	if err != nil || proxyURL != nil {
		t.Fatalf("direct proxy = %v, err = %v", proxyURL, err)
	}
	if err := client.SetProxy("127.0.0.1:7890"); err == nil {
		t.Fatal("proxy without scheme should be rejected")
	}
}
