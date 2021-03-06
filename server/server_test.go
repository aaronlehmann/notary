package server

import (
	"net"
	"strings"
	"testing"

	"github.com/endophage/gotuf/signed"
	"golang.org/x/net/context"
)

func TestRunBadAddr(t *testing.T) {
	err := Run(
		context.Background(),
		"testAddr",
		"../fixtures/ca.pem",
		"../fixtures/ca-key.pem",
		signed.NewEd25519(),
	)
	if err == nil {
		t.Fatal("Passed bad addr, Run should have failed")
	}
}

func TestRunReservedPort(t *testing.T) {
	ctx, _ := context.WithCancel(context.Background())

	err := Run(
		ctx,
		"localhost:80",
		"../fixtures/notary.pem",
		"../fixtures/notary.key",
		signed.NewEd25519(),
	)

	if _, ok := err.(*net.OpError); !ok {
		t.Fatalf("Received unexpected err: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "bind: permission denied") {
		t.Fatalf("Received unexpected err: %s", err.Error())
	}
}
