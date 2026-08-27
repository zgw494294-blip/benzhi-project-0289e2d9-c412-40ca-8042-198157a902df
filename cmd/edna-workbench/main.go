package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"edna-workbench/internal/store"
	"edna-workbench/internal/web"
	"edna-workbench/internal/workflow"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

func run(arguments []string, getenv func(string) string, stdout, stderr io.Writer) error {
	configuration, err := parseConfig(arguments, getenv, stderr)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dataDir := configuration.dataDir
	if configuration.selfcheck {
		dataDir, err = os.MkdirTemp("", "edna-workbench-selfcheck-*")
		if err != nil {
			return fmt.Errorf("创建自检数据目录: %w", err)
		}
		defer os.RemoveAll(dataDir)
	}
	ledger, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer ledger.Close()
	service := workflow.NewService(ledger, time.Now)
	httpServer := &http.Server{
		Addr: configuration.address, Handler: web.NewServer(service, logger).Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	listener, err := net.Listen("tcp", configuration.address)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", configuration.address, err)
	}
	serveError := make(chan error, 1)
	go func() {
		logger.Info("河流 eDNA 工作台已启动", "address", listener.Addr().String(), "dataDir", dataDir)
		serveError <- httpServer.Serve(listener)
	}()
	if configuration.selfcheck {
		err = performSelfcheck(service, listener.Addr().String(), stdout)
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownContext)
		serverErr := <-serveError
		if err != nil {
			return err
		}
		if shutdownErr != nil {
			return fmt.Errorf("关闭自检服务: %w", shutdownErr)
		}
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			return serverErr
		}
		fmt.Fprintln(stdout, "selfcheck: 完整业务流程、HTTP 页面、API 健康检查、账本持久化和凭据验证均通过")
		return nil
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case signalValue := <-stop:
		logger.Info("收到退出信号", "signal", signalValue.String())
	case err := <-serveError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownContext)
}
