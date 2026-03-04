package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

// Init 初始化日志系统
func Init() error {
	// 配置编码器
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // 彩色日志级别
		EncodeTime:     zapcore.ISO8601TimeEncoder,       // ISO8601时间格式
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder, // 短路径
	}

	// 创建核心
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig), // 控制台编码器，支持中文
		zapcore.AddSync(os.Stdout),
		zapcore.DebugLevel, // 设置日志级别
	)

	// 创建日志记录器
	Log = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(0))

	return nil
}

// Sync 同步日志缓冲区
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}

// Info 便捷的Info级别日志
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

// Debug 便捷的Debug级别日志
func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

// Warn 便捷的Warn级别日志
func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

// Error 便捷的Error级别日志
func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

// Fatal 便捷的Fatal级别日志
func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}
