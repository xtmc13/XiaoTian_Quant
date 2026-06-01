package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Level ──

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	}
	return "UNKNOWN"
}

func levelFromString(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	}
	return LevelInfo
}

// ── Log Entry ──

type Entry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Service   string `json:"service"`
	Message   string `json:"message"`
	Caller    string `json:"caller,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// ── Logger ──

// Logger is a structured JSON logger.
type Logger struct {
	service string
	level   Level
	writer  io.Writer
	useJSON bool
	useColor bool
	mu      sync.Mutex
}

var defaultLogger *Logger
var once sync.Once

func initDefault() {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "INFO"
	}
	useJSON := os.Getenv("LOG_FORMAT") == "json"
	useColor := !useJSON

	defaultLogger = &Logger{
		service:  "gateway",
		level:    levelFromString(levelStr),
		writer:   os.Stdout,
		useJSON:  useJSON,
		useColor: useColor,
	}
}

// Get returns the default logger.
func Get() *Logger {
	once.Do(initDefault)
	return defaultLogger
}

// New creates a named logger.
func New(service string) *Logger {
	l := Get()
	return &Logger{
		service:  service,
		level:    l.level,
		writer:   l.writer,
		useJSON:  l.useJSON,
		useColor: l.useColor,
	}
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.level = level
}

// SetOutput sets the log output writer.
func (l *Logger) SetOutput(w io.Writer) {
	l.writer = w
}

func (l *Logger) log(level Level, msg string, fields map[string]any) {
	if level < l.level {
		return
	}

	entry := Entry{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Level:     level.String(),
		Service:   l.service,
		Message:   msg,
		Fields:    fields,
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.useJSON {
		data, _ := json.Marshal(entry)
		fmt.Fprintln(l.writer, string(data))
	} else {
		prefix := fmt.Sprintf("[%s] [%s] [%s]", entry.Timestamp, entry.Level, entry.Service)
		if l.useColor {
			prefix = colorize(level, prefix)
		}
		if len(fields) > 0 {
			fieldStr := ""
			for k, v := range fields {
				fieldStr += fmt.Sprintf(" %s=%v", k, v)
			}
			fmt.Fprintf(l.writer, "%s %s%s\n", prefix, msg, fieldStr)
		} else {
			fmt.Fprintf(l.writer, "%s %s\n", prefix, msg)
		}
	}

	if level == LevelFatal {
		os.Exit(1)
	}
}

func (l *Logger) Debug(msg string, fields ...any) {
	l.log(LevelDebug, msg, toFields(fields...))
}

func (l *Logger) Info(msg string, fields ...any) {
	l.log(LevelInfo, msg, toFields(fields...))
}

func (l *Logger) Warn(msg string, fields ...any) {
	l.log(LevelWarn, msg, toFields(fields...))
}

func (l *Logger) Error(msg string, fields ...any) {
	l.log(LevelError, msg, toFields(fields...))
}

func (l *Logger) Fatal(msg string, fields ...any) {
	l.log(LevelFatal, msg, toFields(fields...))
}

// WithFields returns a new entry builder for structured logging.
func (l *Logger) WithFields(fields map[string]any) *EntryBuilder {
	return &EntryBuilder{logger: l, fields: fields}
}

// EntryBuilder builds a structured log entry.
type EntryBuilder struct {
	logger *Logger
	fields map[string]any
}

func (eb *EntryBuilder) Info(msg string)  { eb.logger.log(LevelInfo, msg, eb.fields) }
func (eb *EntryBuilder) Warn(msg string)  { eb.logger.log(LevelWarn, msg, eb.fields) }
func (eb *EntryBuilder) Error(msg string) { eb.logger.log(LevelError, msg, eb.fields) }
func (eb *EntryBuilder) Debug(msg string) { eb.logger.log(LevelDebug, msg, eb.fields) }

// ── Color output ──

var colors = map[Level]string{
	LevelDebug: "\033[36m", // Cyan
	LevelInfo:  "\033[32m", // Green
	LevelWarn:  "\033[33m", // Yellow
	LevelError: "\033[31m", // Red
	LevelFatal: "\033[35m", // Magenta
}

const colorReset = "\033[0m"

func colorize(level Level, s string) string {
	if c, ok := colors[level]; ok {
		return c + s + colorReset
	}
	return s
}

// ── Helpers ──

func toFields(args ...any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	fields := make(map[string]any)
	for i := 0; i < len(args)-1; i += 2 {
		key := fmt.Sprint(args[i])
		fields[key] = args[i+1]
	}
	return fields
}

// ── Standard log package integration ──

// SetupStdLog redirects the standard log package through this logger.
func SetupStdLog(service string) {
	l := New(service)
	log.SetOutput(&logBridge{logger: l})
	log.SetFlags(0) // We handle timestamps
}

type logBridge struct {
	logger *Logger
}

func (lb *logBridge) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	lb.logger.Info(msg)
	return len(p), nil
}
