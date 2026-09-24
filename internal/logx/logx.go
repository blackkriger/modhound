package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	enabled atomic.Bool
	mu      sync.Mutex
	file    *os.File
	sink    func(string)
)

func Configure(on bool, dir string, lineSink func(string)) error {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
	sink = lineSink
	enabled.Store(false)
	if !on {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "modhound-"+time.Now().Format("2006-01-02")+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	file = f
	enabled.Store(true)
	return nil
}

func Enabled() bool {
	return enabled.Load()
}

func Printf(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	line := time.Now().Format("15:04:05.000 ") + fmt.Sprintf(format, args...)
	mu.Lock()
	if file != nil {
		file.WriteString(line + "\n")
	}
	s := sink
	mu.Unlock()
	if s != nil {
		s(line)
	}
}
