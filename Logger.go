package hubx

import (
	"fmt"
	"io"
)

const (
	Trace = 0
	Debug = 1
	Warn  = 2
	Error = 3
)

var levelmap = map[int]string{
	Trace: "trace",
	Debug: "debug",
	Warn:  "warn",
	Error: "error",
}

type Logger struct {
	writer io.Writer
	level  int
	module string
}

func NewLogger(writer io.Writer, module string, level int) *Logger {
	if writer == nil {
		return nil
	}
	return &Logger{
		writer: writer,
		level:  level,
		module: module,
	}
}

func (l *Logger) format(level, msg string) []byte {
	return []byte(fmt.Sprintf("%s %s %s\n", level, l.module, msg))
}
func (l *Logger) log(level int, msg string) {
	if l == nil || l.writer == nil || level < l.level {
		return
	}
	l.writer.Write(l.format(levelmap[level], msg))
}
func (l *Logger) Trace(msg string) {
	l.log(Trace, msg)
}
func (l *Logger) Debug(msg string) {
	l.log(Debug, msg)
}
func (l *Logger) Warn(msg string) {
	l.log(Warn, msg)
}
func (l *Logger) Error(msg string) {
	l.log(Error, msg)
}
