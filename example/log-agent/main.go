package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Config 定义 Agent 的运行配置
type Config struct {
	LogPattern string // 日志文件匹配模式，例如 "logs/*.log"
	BatchSize  int    // 批量写入数据库的大小
	DryRun     bool   // 如果为 true，仅打印到控制台，不写入数据库
}

func main() {
	// 1. 配置 zerolog 用于 Agent 自身的日志输出
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})

	// 2. 解析命令行参数
	cfg := Config{}
	flag.StringVar(&cfg.LogPattern, "pattern", "../logs/*.log", "Glob pattern for log files to ingest")
	flag.IntVar(&cfg.BatchSize, "batch", 100, "Batch size for database insertion")
	flag.BoolVar(&cfg.DryRun, "dry-run", true, "Print parsed logs to stdout instead of inserting into DB")
	flag.Parse()

	log.Info().Msgf("Starting Log Agent. Pattern: %s, DryRun: %v", cfg.LogPattern, cfg.DryRun)

	// 3. 查找匹配的日志文件
	files, err := filepath.Glob(cfg.LogPattern)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to glob log files")
	}
	if len(files) == 0 {
		log.Warn().Msg("No log files found matching the pattern.")
		return
	}
	log.Info().Int("count", len(files)).Msg("Found log files")

	// 4. 初始化管道
	// entriesChan 用于传输解析后的 LogEntry
	entriesChan := make(chan *LogEntry, cfg.BatchSize*2)

	// 上下文用于优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5. 启动 Consumer (模拟数据库写入器)
	var wgConsumer sync.WaitGroup
	wgConsumer.Add(1)
	go func() {
		defer wgConsumer.Done()
		runConsumer(ctx, cfg, entriesChan)
	}()

	// 6. 启动 Producers (文件解析器)
	var wgProducers sync.WaitGroup
	for _, file := range files {
		wgProducers.Add(1)
		go func(f string) {
			defer wgProducers.Done()
			log.Info().Str("file", f).Msg("Parsing file...")
			// 调用 parser.go 中的 ParseLogFile
			ParseLogFile(f, entriesChan)
			log.Info().Str("file", f).Msg("Finished parsing file")
		}(file)
	}

	// 7. 处理优雅退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待生产者完成 或 收到退出信号
	doneProducers := make(chan struct{})
	go func() {
		wgProducers.Wait()
		close(doneProducers)
	}()

	select {
	case <-doneProducers:
		log.Info().Msg("All log files parsed successfully.")
	case <-sigChan:
		log.Warn().Msg("Received shutdown signal, stopping agent...")
		cancel() // 通知 consumer 停止
	}

	// 关闭 channel，通知 Consumer 没有更多数据了
	close(entriesChan)

	// 等待 Consumer 处理完剩余数据
	wgConsumer.Wait()
	log.Info().Msg("Log Agent exit.")
}

// runConsumer 模拟数据库批量写入逻辑
func runConsumer(ctx context.Context, cfg Config, ch <-chan *LogEntry) {
	var batch []*LogEntry

	// 模拟数据库插入的函数
	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		if cfg.DryRun {
			// DryRun 模式：简单打印统计信息和第一条数据
			log.Info().Int("batch_size", len(batch)).Msg("Simulating DB Insert")
			// 打印第一条数据展示解析结果
			fmt.Printf("  [DryRun Sample] Time: %s, Level: %s, Msg: %s\n",
				batch[0].Timestamp.Format(time.RFC3339),
				batch[0].Level,
				batch[0].Message,
			)
		} else {
			// 真实模式：这里应该调用 gorm.DB.Create(&batch)
			// db.CreateInBatches(batch, 100)
			log.Info().Int("count", len(batch)).Msg("Inserted records into Database")
		}

		// 清空缓冲区
		batch = batch[:0] // keep capacity
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				// Channel closed, flush remaining and exit
				flushBatch()
				return
			}
			batch = append(batch, entry)
			if len(batch) >= cfg.BatchSize {
				flushBatch()
			}
		case <-ticker.C:
			// 定时刷新，防止数据滞留
			flushBatch()
		case <-ctx.Done():
			// 上下文取消，尽最大努力刷新
			flushBatch()
			return
		}
	}
}
