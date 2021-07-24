package discovery

import (
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRegister(t *testing.T) {
	info := Server{
		Name:    "user",
		Addr:    "localhost:8083",
		Version: "1.0.0",
		Weight:  2,
	}

	addrs := []string{"127.0.0.1:2379"}
	r := NewRegister(addrs, zap.NewNop())

	_, err := r.Register(info, 2)
	if err != nil {
		t.Fatalf("register to etcd failed %v", err)
	}

	infoRes, err := r.GetServerInfo()
	if err != nil {
		t.Fatalf("get info failed %v", err)
	}
	log.Println(infoRes)
	time.Sleep(2 * time.Second)

	req, err := http.NewRequest("GET", "/weight?weight=3", nil)
	if err != nil {
		t.Fatalf("init request failed: %v", err)
	}
	rr := httptest.NewRecorder()
	r.UpdateHandler().ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	infoRes, err = r.GetServerInfo()
	if err != nil {
		t.Fatalf("get info failed %v", err)
	}
	log.Println(infoRes)
	if infoRes.Weight != 3 {
		t.Fatal("update weight error")
	}

	time.Sleep(5 * time.Second)

	//r.Stop()
}
