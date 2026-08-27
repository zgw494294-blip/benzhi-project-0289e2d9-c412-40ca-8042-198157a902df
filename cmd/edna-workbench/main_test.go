package main

import (
	"io"
	"testing"
)

func TestParseConfigAddressPrecedence(t *testing.T) {
	configuration, err := parseConfig(nil, func(key string) string {
		if key == "PORT" {
			return "19123"
		}
		return ""
	}, io.Discard)
	if err != nil || configuration.address != "127.0.0.1:19123" {
		t.Fatalf("PORT 未绑定回环地址: config=%+v err=%v", configuration, err)
	}
	configuration, err = parseConfig([]string{"-addr=127.0.0.1:19234"}, func(string) string { return "19123" }, io.Discard)
	if err != nil || configuration.address != "127.0.0.1:19234" {
		t.Fatalf("显式 -addr 应优先: config=%+v err=%v", configuration, err)
	}
}

func TestRejectUnsafeAddress(t *testing.T) {
	for _, address := range []string{"0.0.0.0:19081", "127.0.0.1:8080", ":19081", "192.168.1.2:19081", "127.0.0.1:80"} {
		if err := validateAddress(address); err == nil {
			t.Errorf("地址 %s 应被拒绝", address)
		}
	}
}
