package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultAddress = "127.0.0.1:19081"

type config struct {
	address   string
	dataDir   string
	selfcheck bool
}

func parseConfig(arguments []string, getenv func(string) string, stderr io.Writer) (config, error) {
	set := flag.NewFlagSet("edna-workbench", flag.ContinueOnError)
	set.SetOutput(stderr)
	address := set.String("addr", defaultAddress, "HTTP 监听地址，必须为回环地址和高位端口")
	dataDir := set.String("data", "./data", "事件账本与快照目录")
	selfcheck := set.Bool("selfcheck", false, "执行有界完整业务流程并主动退出")
	if err := set.Parse(arguments); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("不支持额外参数: %s", strings.Join(set.Args(), " "))
	}
	addressExplicit := false
	set.Visit(func(item *flag.Flag) {
		if item.Name == "addr" {
			addressExplicit = true
		}
	})
	resolvedAddress := strings.TrimSpace(*address)
	if !addressExplicit {
		if portText := strings.TrimSpace(getenv("PORT")); portText != "" {
			port, err := strconv.Atoi(portText)
			if err != nil {
				return config{}, errors.New("PORT 必须是十进制端口号")
			}
			resolvedAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		}
	}
	if err := validateAddress(resolvedAddress); err != nil {
		return config{}, err
	}
	cleanDataDir := filepath.Clean(strings.TrimSpace(*dataDir))
	if cleanDataDir == "." || cleanDataDir == string(filepath.Separator) {
		return config{}, errors.New("数据目录不能是工作区根目录或文件系统根目录")
	}
	return config{address: resolvedAddress, dataDir: cleanDataDir, selfcheck: *selfcheck}, nil
}

func validateAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("-addr 必须为 host:port: %w", err)
	}
	addressIP := net.ParseIP(host)
	if addressIP == nil || !addressIP.IsLoopback() {
		return errors.New("监听地址必须使用回环 IP，禁止 0.0.0.0、空主机和外部网卡地址")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1024 || port > 65535 {
		return errors.New("监听端口必须是 1024 到 65535 之间的高位端口")
	}
	if port == 3000 || port == 8080 {
		return errors.New("禁止使用常见开发端口 3000 或 8080")
	}
	return nil
}
