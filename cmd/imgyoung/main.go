// ImgYoung 图床服务入口:装配配置、数据库、存储驱动与 HTTP 服务。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/x-haose/imgyoung/internal/config"
	"github.com/x-haose/imgyoung/internal/db"
	"github.com/x-haose/imgyoung/internal/server"
	"github.com/x-haose/imgyoung/internal/storage"
	"github.com/x-haose/imgyoung/internal/upload"
)

// shutdownTimeout 优雅退出时等待 in-flight 请求完成的上限。
const shutdownTimeout = 10 * time.Second

// readHeaderTimeout 读取请求头的上限,防慢速连接占用。
const readHeaderTimeout = 10 * time.Second

// main 进程入口,失败时以非零码退出。
func main() {
	if err := run(); err != nil {
		slog.Error("服务退出", "err", err)
		os.Exit(1)
	}
}

// run 装配全部依赖并运行 HTTP 服务,直至收到退出信号或服务异常。
// @Description: 最小优雅退出:停收新请求 + 等待 in-flight 请求完成;
// 完整停机编排(排空任务/checkpoint WAL)见设计定稿·优雅停机。
// @return 启动失败或异常退出时的错误
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("创建数据目录 %q: %w", cfg.DataDir, err)
	}

	conn, err := db.Open(filepath.Join(cfg.DataDir, "imgyoung.db"))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Error("关闭数据库失败", "err", cerr)
		}
	}()
	if err = db.Migrate(conn); err != nil {
		return err
	}

	driver, err := storage.NewRcloneDriver(ctx, filepath.Join(cfg.DataDir, "blobs"))
	if err != nil {
		return err
	}

	token := cfg.Token
	if token == "" {
		if token, err = config.GenerateToken(); err != nil {
			return err
		}
		logger.Info("未配置 IMGYOUNG_TOKEN,已生成随机上传 Token(仅本次运行有效)", "token", token)
	}

	queries := db.New(conn)
	uploadSvc := upload.New(upload.Config{
		Conn:      conn,
		Queries:   queries,
		Driver:    driver,
		MaxPixels: cfg.MaxPixels,
	})
	srv := server.New(server.Config{
		Queries:              queries,
		Driver:               driver,
		Upload:               uploadSvc,
		Token:                token,
		MaxUploadBytes:       cfg.MaxUploadBytes,
		MaxConcurrentUploads: cfg.MaxConcurrentUploads,
		BaseURL:              cfg.BaseURL,
		DefaultEXIFPolicy:    cfg.EXIFPolicy,
		RefererWhitelist:     cfg.RefererWhitelist,
		AllowEmptyReferer:    cfg.AllowEmptyReferer,
		Logger:               logger,
	})
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	logger.Info("ImgYoung 已启动", "listen", cfg.Listen, "data_dir", cfg.DataDir)

	select {
	case err = <-errCh:
		return fmt.Errorf("HTTP 服务异常退出: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err = httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅退出: %w", err)
	}
	if err = <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 服务收尾: %w", err)
	}
	logger.Info("已退出")
	return nil
}
