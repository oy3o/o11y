package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogFileParser_ParseLine 是对核心解析逻辑的单元测试
func TestLogFileParser_ParseLine(t *testing.T) {
	// --- 定义测试用的时间和样本数据 ---
	baseTime := time.Date(2025, 11, 18, 10, 30, 0, 0, time.UTC)
	expectedEntryBase := &LogEntry{
		Environment: "production",
		Version:     "v1.2.3",
		Service:     "user-service",
		Module:      "login",
		Trace:       "trace-id-123",
		User:        "user-id-456",
		Span:        "span-id-789",
		Level:       "info",
		Message:     "User logged in successfully",
		Timestamp:   baseTime,
		Attributes:  map[string]interface{}{"login_method": "password"},
	}

	testCases := []struct {
		name string
		// logLineFn 用于生成日志行，以便我们可以动态地插入不同的时间戳
		logLineFn           func() []byte
		expected            *LogEntry
		expectErr           bool
		expectedErrContains string
	}{
		{
			name: "Should_parse_full_log_with_seconds_precision",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":         baseTime.Unix(),
					"level":        "info",
					"message":      "User logged in successfully",
					"environment":  "production",
					"version":      "v1.2.3",
					"service":      "user-service",
					"module":       "login",
					"trace":        "trace-id-123",
					"user":         "user-id-456",
					"span":         "span-id-789",
					"login_method": "password",
				})
			},
			expected:  expectedEntryBase,
			expectErr: false,
		},
		{
			name: "Should_parse_log_with_milliseconds_precision",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":         baseTime.UnixMilli(),
					"level":        "info",
					"message":      "User logged in successfully",
					"environment":  "production",
					"version":      "v1.2.3",
					"service":      "user-service",
					"module":       "login",
					"trace":        "trace-id-123",
					"user":         "user-id-456",
					"span":         "span-id-789",
					"login_method": "password",
				})
			},
			expected:  expectedEntryBase,
			expectErr: false,
		},
		{
			name: "Should_parse_log_with_microseconds_precision",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":         baseTime.UnixMicro(),
					"level":        "info",
					"message":      "User logged in successfully",
					"environment":  "production",
					"version":      "v1.2.3",
					"service":      "user-service",
					"module":       "login",
					"trace":        "trace-id-123",
					"user":         "user-id-456",
					"span":         "span-id-789",
					"login_method": "password",
				})
			},
			expected:  expectedEntryBase,
			expectErr: false,
		},
		{
			name: "Should_parse_log_with_nanoseconds_precision",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":         baseTime.UnixNano(),
					"level":        "info",
					"message":      "User logged in successfully",
					"environment":  "production",
					"version":      "v1.2.3",
					"service":      "user-service",
					"module":       "login",
					"trace":        "trace-id-123",
					"user":         "user-id-456",
					"span":         "span-id-789",
					"login_method": "password",
				})
			},
			expected:  expectedEntryBase,
			expectErr: false,
		},
		{
			name: "Should_handle_missing_optional_fields_and_empty_attributes",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":    baseTime.Unix(),
					"level":   "warn",
					"message": "A simple warning",
					"service": "test-service",
				})
			},
			expected: &LogEntry{
				Service:    "test-service",
				Level:      "warn",
				Message:    "A simple warning",
				Timestamp:  baseTime,
				Attributes: nil, // 确保空的 attributes 被设置为 nil
			},
			expectErr: false,
		},
		{
			name: "Should_return_error_for_malformed_json",
			logLineFn: func() []byte {
				return []byte(`{"time": 123, "message": "hello"`) // 缺少右括号
			},
			expectErr:           true,
			expectedErrContains: "failed to decode json",
		},
		{
			name: "Should_return_error_if_time_field_is_missing",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"message": "hello",
				})
			},
			expectErr:           true,
			expectedErrContains: "failed to determine timestamp precision",
		},
		{
			name: "Should_return_error_for_unknown_timestamp_precision",
			logLineFn: func() []byte {
				return createLogLine(map[string]interface{}{
					"time":    12345, // 5位，不属于任何已知精度
					"message": "hello",
				})
			},
			expectErr:           true,
			expectedErrContains: "unexpected timestamp magnitude",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 为每个测试用例创建一个新的解析器，以保证状态隔离
			parser := NewLogFileParser()
			logLine := tc.logLineFn()

			entry, err := parser.ParseLine(logLine)

			if tc.expectErr {
				require.Error(t, err)
				if tc.expectedErrContains != "" {
					assert.Contains(t, err.Error(), tc.expectedErrContains)
				}
			} else {
				expected := *tc.expected
				if tc.expected.Attributes != nil {
					expected.Attributes = make(map[string]interface{})
					for k, v := range tc.expected.Attributes {
						expected.Attributes[k] = v
					}
				}

				require.NoError(t, err)
				require.NotNil(t, entry)

				// 对比时间时，使用 time.Equal() 是最佳实践
				assert.True(t, expected.Timestamp.Equal(entry.Timestamp), "Timestamp mismatch")
				// 将时间置零，以便使用 assert.Equal 对比结构体的其余部分
				expected.Timestamp = time.Time{}
				entry.Timestamp = time.Time{}

				// 因为 entry 是指针，所以我们需要比较 &expected
				assert.Equal(t, &expected, entry)
			}
		})
	}
}

// TestParseLogFile 是对文件级解析函数的集成测试
func TestParseLogFile(t *testing.T) {
	// 1. 准备一个临时日志文件
	logContent := fmt.Sprintf(`{"time": %d, "level": "info", "message": "First line"}
{"time": %d, "level": "error", "message": "Second line", "error": "file not found"}
`, time.Now().UnixMilli(), time.Now().Add(1*time.Second).UnixMilli())

	tempDir := t.TempDir()
	logFilePath := filepath.Join(tempDir, "integration.log")
	err := os.WriteFile(logFilePath, []byte(logContent), 0o644)
	require.NoError(t, err)

	// 2. 准备一个 channel 来接收结果
	entriesChan := make(chan *LogEntry, 5)

	// 3. 执行解析
	ParseLogFile(logFilePath, entriesChan)
	close(entriesChan) // 关闭 channel 以便我们可以遍历它

	// 4. 断言结果
	var results []*LogEntry
	for entry := range entriesChan {
		results = append(results, entry)
	}

	require.Len(t, results, 2)
	assert.Equal(t, "info", results[0].Level)
	assert.Equal(t, "First line", results[0].Message)
	assert.Equal(t, "error", results[1].Level)
	assert.Equal(t, "Second line", results[1].Message)
	require.NotNil(t, results[1].Error)
	assert.Equal(t, "file not found", *results[1].Error)
}

// createLogLine 是一个辅助函数，用于将 map 转换为 JSON 字节切片
func createLogLine(data map[string]interface{}) []byte {
	bytes, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("test setup failed: could not marshal log line: %v", err))
	}
	return bytes
}
