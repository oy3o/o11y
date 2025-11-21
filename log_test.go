package o11y_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/oy3o/o11y"
)

// TestInit_Logging_Configuration 用于测试日志的各种配置组合
func TestInit_Logging_Configuration(t *testing.T) {
	// 保存原始的 zerolog 全局 Logger，以便在测试结束后恢复
	originalLogger := zerolog.DefaultContextLogger

	t.Cleanup(func() {
		// t.Cleanup 会在该测试函数（及其所有子测试）完成后执行
		zerolog.DefaultContextLogger = originalLogger
		zerolog.SetGlobalLevel(zerolog.DebugLevel) // 恢复到默认的宽松级别
	})

	// --- 定义我们的测试用例 ---
	testCases := []struct {
		name          string                                   // 测试用例的名称
		config        o11y.Config                              // 输入的配置
		logAction     func()                                   // 在 Init 后执行的日志记录操作
		assertConsole func(t *testing.T, consoleOutput string) // 对控制台输出的断言
		assertFile    func(t *testing.T, fileContent string)   // 对文件输出的断言
	}{
		{
			name: "Should_log_to_console_only_in_human-readable_format",
			config: o11y.Config{
				Enabled: true,
				Log: o11y.LogConfig{
					Level:         "info",
					EnableConsole: true,
					EnableFile:    false,
				},
			},
			logAction: func() {
				// 使用 zerolog.New(writer).Msg() 来模拟全局 log
				log.Info().Msg("hello console")
			},
			assertConsole: func(t *testing.T, output string) {
				assert.Contains(t, output, "INF") // ConsoleWriter 的标志
				assert.Contains(t, output, "hello console")
				assert.NotContains(t, output, "{") // 不应该是 JSON 格式
			},
			assertFile: func(t *testing.T, content string) {
				assert.Empty(t, content, "File should not be written to")
			},
		},
		{
			name: "Should_log_to_both_console_and_file",
			config: o11y.Config{
				Enabled: true,
				Log: o11y.LogConfig{
					Level:         "warn",
					EnableConsole: true,
					EnableFile:    true,
				},
			},
			logAction: func() {
				log.Warn().Msg("disk space low")
			},
			assertConsole: func(t *testing.T, output string) {
				assert.Contains(t, output, "WRN")
				assert.Contains(t, output, "disk space low")
			},
			assertFile: func(t *testing.T, content string) {
				assert.Contains(t, content, `"level":"warn"`)
				assert.Contains(t, content, `"message":"disk space low"`)
			},
		},
	}

	// --- 循环执行测试用例 ---
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// --- 准备工作 (Setup) ---
			// 1. 准备临时文件用于日志写入
			tempDir := t.TempDir() // 使用 t.TempDir() 可以确保测试后自动清理
			if tc.config.Log.EnableFile {
				tc.config.Log.FileRotation.Filename = filepath.Join(tempDir, "test.log")
			}

			// 2. 捕获控制台输出
			// 重定向 os.Stdout
			// 这是一个更可靠的方法来捕获所有写入标准输出的内容
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			t.Cleanup(func() { os.Stdout = oldStdout })

			// --- 执行测试 ---
			// 调用 Init 来设置全局 Logger
			shutdown := o11y.Init(tc.config)

			// 执行日志记录操作
			tc.logAction()

			// --- 断言 (Assertions) ---
			// 1. 恢复标准输出并读取内容
			w.Close()
			os.Stdout = oldStdout
			consoleOutputBytes, _ := io.ReadAll(r)
			consoleOutput := string(consoleOutputBytes)

			// 2. 如果需要，读取文件内容
			var fileContent string
			if tc.config.Log.EnableFile {
				// 在读取前，先调用 shutdown 确保所有缓冲都已写入文件
				err := shutdown(context.Background())
				require.NoError(t, err)

				fileBytes, err := os.ReadFile(tc.config.Log.FileRotation.Filename)
				// 如果文件不存在，ReadFile 会返回错误，这对于某些测试是预期的
				if err == nil {
					fileContent = string(fileBytes)
				}
			} else {
				// 确保即使不写文件，shutdown也能正常工作
				err := shutdown(context.Background())
				require.NoError(t, err)
			}

			// 3. 执行断言函数
			tc.assertConsole(t, consoleOutput)
			tc.assertFile(t, fileContent)
		})
	}
}

// TestInit_Logging_Level 用于单独测试日志级别的设置是否正确
func TestInit_Logging_Level(t *testing.T) {
	// 保存并恢复原始的全局 Logger 和 Level
	originalLogger := log.Logger
	originalLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		log.Logger = originalLogger
		zerolog.SetGlobalLevel(originalLevel)
	})

	testCases := []struct {
		name          string
		levelStr      string
		expectedLevel zerolog.Level
		expectWarning bool
	}{
		{"Should_set_debug_level", "debug", zerolog.DebugLevel, false},
		{"Should_set_info_level", "info", zerolog.InfoLevel, false},
		{"Should_set_warn_level", "warn", zerolog.WarnLevel, false},
		{"Should_set_error_level", "error", zerolog.ErrorLevel, false},
		{"Should_default_to_info_for_invalid_level", "invalid_level", zerolog.InfoLevel, true},
		{"Should_default_to_info_for_empty_level", "", zerolog.InfoLevel, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			initialSubTestLevel := zerolog.GlobalLevel()
			t.Cleanup(func() {
				zerolog.SetGlobalLevel(initialSubTestLevel)
			})

			// 1. 创建一个内存缓冲区来捕获任何意外的日志输出
			var logBuffer bytes.Buffer
			// 2. 临时将全局 logger 指向这个 buffer，以捕获 setupLogging 中的警告

			log.Logger = zerolog.New(&logBuffer)

			cfg := o11y.Config{
				Enabled: true,
				Log: o11y.LogConfig{
					// 我们不希望这个测试写入任何地方，所以都设置为 false
					EnableConsole: false,
					EnableFile:    false,
					Level:         tc.levelStr,
				},
			}

			shutdown := o11y.Init(cfg)
			// 在这个测试中，shutdown 没什么用，但调用它是个好习惯
			defer shutdown(context.Background())

			// 断言全局级别是否被正确设置
			assert.Equal(t, tc.expectedLevel, zerolog.GlobalLevel())

			// 如果我们预期有警告，就检查缓冲区
			if tc.expectWarning {
				assert.Contains(t, logBuffer.String(), "Invalid or empty log level")
			} else {
				assert.Empty(t, logBuffer.String())
			}
		})
	}
}
