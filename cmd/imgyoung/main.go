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

	"github.com/x-haose/imgyoung/internal/backup"
	"github.com/x-haose/imgyoung/internal/core/channel"
	"github.com/x-haose/imgyoung/internal/core/upload"
	"github.com/x-haose/imgyoung/internal/infra/config"
	"github.com/x-haose/imgyoung/internal/infra/db"
	"github.com/x-haose/imgyoung/internal/infra/secret"
	"github.com/x-haose/imgyoung/internal/server"
	"github.com/x-haose/imgyoung/internal/task"
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

	master, err := loadMasterKey(cfg, logger)
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
	channels, err := channel.NewManager(ctx, channel.ManagerConfig{
		Conn:    conn,
		Queries: queries,
		Master:  master,
		DataDir: cfg.DataDir,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	runner, err := task.NewRunner(task.RunnerConfig{
		Conn:    conn,
		Queries: queries,
		Manager: channels,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	// Runner 用独立生命周期:SIGTERM 不直接砍断在途复制,由 Stop 优雅收敛。
	runnerCtx, runnerCancel := context.WithCancel(context.Background())
	defer runnerCancel()
	if err = runner.Start(runnerCtx); err != nil {
		return err
	}

	uploadSvc := upload.New(upload.Config{
		Conn:       conn,
		Queries:    queries,
		Channels:   channels,
		Replicator: runner,
		MaxPixels:  cfg.MaxPixels,
		MinSuccess: cfg.MinSuccess,
		Logger:     logger,
	})
	srv := server.New(server.Config{
		Queries:              queries,
		Channels:             channels,
		Admin:                channels,
		EncryptNewChannels:   cfg.EncryptNewChannels,
		RedirectMode:         cfg.AccessMode == "redirect",
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

	// 渠道健康检查循环(设计定稿·健康检查+自动切换)。
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.HealthCheckSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				changed, cerr := channels.CheckAll(probeCtx)
				cancel()
				if cerr != nil {
					logger.Warn("健康检查未完成", "err", cerr)
				}
				if len(changed) > 0 {
					logger.Info("渠道健康状态变化", "channels", changed)
				}
			}
		}
	}()

	// 元数据库自动备份循环(设计定稿·可靠性与灾备)。
	backupSvc := backup.New(backup.Config{
		Conn:     conn,
		Channels: channels,
		DataDir:  cfg.DataDir,
		Keep:     cfg.BackupKeep,
		Logger:   logger,
	})
	go backupSvc.Run(ctx, time.Duration(cfg.BackupIntervalHours)*time.Hour)

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
	// 任务停止使用独立预算,不与 HTTP 排空争抢同一超时。
	runnerStopCtx, cancelRunnerStop := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelRunnerStop()
	if err = runner.Stop(runnerStopCtx); err != nil {
		logger.Warn("补齐任务停止未完成", "err", err)
	}
	runnerCancel()
	if err = <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP 服务收尾: %w", err)
	}
	logger.Info("已退出")
	return nil
}

// loadMasterKey 解析主密钥:环境变量/配置(hex)优先,否则数据目录文件(首启生成)。
// @param cfg 运行配置
// @param logger 结构化日志(首启生成时提示备份)
// @return 主密钥
// @return 密钥非法或文件读写失败时的错误
func loadMasterKey(cfg config.Config, logger *slog.Logger) (*secret.MasterKey, error) {
	if cfg.MasterKey != "" {
		key, err := secret.MasterKeyFromHex(cfg.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("master_key 配置非法: %w", err)
		}
		return key, nil
	}
	key, created, err := secret.LoadOrCreate(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if created {
		logger.Warn("已生成主密钥文件,请立即备份——丢失将导致渠道配置与加密渠道数据永久不可恢复",
			"path", filepath.Join(cfg.DataDir, "master.key"))
	}
	return key, nil
}
