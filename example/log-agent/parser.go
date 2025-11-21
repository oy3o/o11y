package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LogEntry 对应我们设计的数据库表结构, 使用了 gorm 的约定来定义列属性和索引
type LogEntry struct {
	// GORM 会自动将 ID 字段作为主键
	ID uint `gorm:"primarykey"`

	// --- 核心元数据 ---
	// 在 GORM 中, CreatedAt 字段会自动在创建时填充当前时间
	// 我们用 `index` tag 为它创建了一个复合索引的一部分
	Timestamp   time.Time `json:"time" gorm:"not null;index:idx_ts_svc,priority:1"`
	Environment string    `json:"environment,omitempty" gorm:"index"`
	Service     string    `json:"service" gorm:"not null;index:idx_ts_svc,priority:2"`
	Version     string    `json:"version,omitempty"`
	Module      string    `json:"module,omitempty" gorm:"index:idx_svc_mod"`

	// --- 追踪与上下文 ---
	Trace string `json:"trace,omitempty" gorm:"index"`
	Span  string `json:"span,omitempty"`
	User  string `json:"user,omitempty" gorm:"column:user_id;index"`

	// --- 日志内容 ---
	Level   string `json:"level" gorm:"not null;index"`
	Message string `json:"message"`
	// 使用 type:jsonb 来指定 PostgreSQL 的 JSONB 类型
	// 使用 class:gin 来指定 GIN 索引, 这是查询 JSON 的最佳选择
	Attributes map[string]any `json:"attributes,omitempty" gorm:"type:jsonb;index:idx_attrs_gin,class:gin"`

	// --- 额外信息 ---
	// *string 类型在 GORM 中默认就是 nullable, 无需额外 tag
	Caller *string `json:"caller,omitempty"`
	Error  *string `json:"error,omitempty"`
	Stack  *string `json:"stack,omitempty"`
}

// ParseLogFile 解析一个日志文件, 并将结果放入目标队列
func ParseLogFile(filePath string, entriesChan chan<- *LogEntry) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	// 为这个文件创建一个专属的解析器
	parser := NewLogFileParser()

	scanner := bufio.NewScanner(file)
	// 增加 buffer size 防止超长行导致 panic
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// 使用解析器对象的方法，而不是全局函数
		entry, err := parser.ParseLine(line)
		if err != nil {
			// 只有在第一次检测失败时才会出错，后续基本不会
			fmt.Fprintf(os.Stderr, "Error parsing line in %s: %v\n", filePath, err)
			continue
		}
		entriesChan <- entry
	}
}

// TimestampPrecision 是一个枚举类型，用于表示检测到的时间戳精度
type TimestampPrecision int

const (
	PrecisionUnknown TimestampPrecision = iota // 初始状态
	PrecisionSeconds
	PrecisionMillis
	PrecisionMicros
	PrecisionNanos
)

// LogFileParser 是一个有状态的解析器，用于处理单个日志文件
type LogFileParser struct {
	// 存储检测到的精度
	precision TimestampPrecision

	// 存储一个直接的转换函数指针，避免每次都 switch
	tsParser func(tsInt int64) time.Time
}

// NewLogFileParser 创建一个新的解析器实例
func NewLogFileParser() *LogFileParser {
	return &LogFileParser{
		precision: PrecisionUnknown,
		tsParser:  nil, // 初始为空
	}
}

// ParseLine 解析单行日志。它会在第一次调用时检测并设置精度。
func (p *LogFileParser) ParseLine(line []byte) (*LogEntry, error) {
	var rawLog map[string]any

	// 使用 Decoder 并开启 UseNumber
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	if err := decoder.Decode(&rawLog); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	// 如果精度未知，则进行一次性检测
	if p.precision == PrecisionUnknown {
		if err := p.detectAndSetPrecision(rawLog); err != nil {
			// 如果第一行就无法确定精度，可以返回错误或跳过
			return nil, fmt.Errorf("failed to determine timestamp precision: %w", err)
		}
	}

	// 使用已经确定的解析器进行转换
	entry, err := p.mapToLogEntry(rawLog)
	if err != nil {
		return nil, err
	}

	return entry, nil
}

// detectAndSetPrecision 从原始日志中检测时间戳精度并设置解析器状态
func (p *LogFileParser) detectAndSetPrecision(rawLog map[string]any) error {
	tsValue, ok := rawLog["time"]
	if !ok {
		return fmt.Errorf("'time' field not found")
	}

	// When using Unmarshal without UseNumber, numbers are float64
	tsFloat, ok := tsValue.(json.Number)
	if !ok {
		return fmt.Errorf("'time' field is not a number, but %T", tsValue)
	}

	// Format float to string to check magnitude (digits before decimal)
	tsStr := tsFloat.String()

	switch len(tsStr) {
	case 10:
		p.precision = PrecisionSeconds
		p.tsParser = func(ts int64) time.Time { return time.Unix(ts, 0) }
	case 13:
		p.precision = PrecisionMillis
		p.tsParser = func(ts int64) time.Time { return time.Unix(0, ts*int64(time.Millisecond)) }
	case 16:
		p.precision = PrecisionMicros
		p.tsParser = func(ts int64) time.Time { return time.Unix(0, ts*int64(time.Microsecond)) }
	case 19:
		p.precision = PrecisionNanos
		p.tsParser = func(ts int64) time.Time { return time.Unix(0, ts) }
	default:
		return fmt.Errorf("unexpected timestamp magnitude (digits: %d, val: %s)", len(tsStr), tsStr)
	}

	return nil
}

// mapToLogEntry 使用已知的精度解析器将原始日志映射到 LogEntry 结构
func (p *LogFileParser) mapToLogEntry(rawLog map[string]any) (*LogEntry, error) {
	entry := &LogEntry{
		Attributes: make(map[string]any),
	}

	for key, value := range rawLog {
		// Helper to handle potential nil values gracefully if needed
		if value == nil {
			continue
		}

		switch key {
		case "environment":
			entry.Environment, _ = value.(string)
		case "version":
			entry.Version, _ = value.(string)
		case "service":
			entry.Service, _ = value.(string)
		case "module":
			entry.Module, _ = value.(string)
		case "trace":
			entry.Trace, _ = value.(string)
		case "user":
			entry.User, _ = value.(string)
		case "span":
			entry.Span, _ = value.(string)
		case "level":
			entry.Level, _ = value.(string)
		case "message":
			entry.Message, _ = value.(string)
		case "caller":
			if s, ok := value.(string); ok {
				entry.Caller = &s
			}
		case "error":
			if s, ok := value.(string); ok {
				entry.Error = &s
			}
		case "stack":
			if s, ok := value.(string); ok {
				entry.Stack = &s
			}
		case "time":
			// Handle float64 from Unmarshal
			tsFloat, ok := value.(json.Number)
			if !ok {
				return nil, fmt.Errorf("internal error: expected 'time' to be number")
			}
			tsInt, err := tsFloat.Int64()
			if err != nil {
				return nil, err
			}
			time := p.tsParser(tsInt).UTC()
			entry.Timestamp = time
		default:
			// 所有未知的字段都放入 attributes JSON blob 中
			entry.Attributes[key] = value
		}
	}

	// 如果 attributes 为空，可以将其设为 nil 以节省空间
	if len(entry.Attributes) == 0 {
		entry.Attributes = nil
	}

	return entry, nil
}
