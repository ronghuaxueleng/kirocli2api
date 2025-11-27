package Utils

import (
	"io"
	"log"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	ErrorLogger  *log.Logger
	NormalLogger *log.Logger
)

func InitLoggers() {
	err := os.MkdirAll("resources", 0755)
	if err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
		return
	}

	errFile := &lumberjack.Logger{
		Filename:   "resources/error.log",
		MaxSize:    50,
		MaxAge:     1,
		MaxBackups: 3,
		Compress:   true,
	}
	ErrorLogger = log.New(errFile, "", log.LstdFlags)

	normalFile := &lumberjack.Logger{
		Filename:   "resources/normal.log",
		MaxSize:    50,
		MaxAge:     1,
		MaxBackups: 3,
		Compress:   true,
	}
	NormalLogger = log.New(io.MultiWriter(normalFile, os.Stdout), "", log.LstdFlags)
}

func LogRequestError(req string, mapped interface{}) {
	ErrorLogger.Printf("Req: %s\nMapped: %+v", req, mapped)
}
