package logger

import (
	"log"
	"os"
)

var (
	InfoLogger  = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	WarnLogger  = log.New(os.Stdout, "WARN: ", log.Ldate|log.Ltime)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func Info(message string) {
	InfoLogger.Println(message)
}

func Warn(message string) {
	WarnLogger.Println(message)
}

func Error(message string) {
	ErrorLogger.Println(message)
}
