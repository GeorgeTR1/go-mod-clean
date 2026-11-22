package main

import (
   "bufio"
   "sync"
   "time"
   "io"
)

const flushLatency = 30 * time.Millisecond
const bufSize = 8 * 1024

// based on https://cs.opensource.google/go/go/+/refs/tags/go1.26.4:src/net/http/httputil/reverseproxy.go;l=764
type FlushWriter struct {
   dst     *bufio.Writer
   mu      sync.Mutex
   t       *time.Timer
   flushPending bool
}

func (m *FlushWriter) Write(p []byte) (n int, err error) {
   m.mu.Lock()
   defer m.mu.Unlock()
   n, err = m.dst.Write(p)
   if m.flushPending {
      return
   }
   if m.t == nil {
      m.t = time.AfterFunc(flushLatency, m.delayedFlush)
   } else {
      m.t.Reset(flushLatency)
   }
   m.flushPending = true
   return
}

func (m *FlushWriter) WriteString(s string) (n int, err error) {
   m.mu.Lock()
   defer m.mu.Unlock()
   n, err = m.dst.WriteString(s)
   if m.flushPending {
      return
   }
   if m.t == nil {
      m.t = time.AfterFunc(flushLatency, m.delayedFlush)
   } else {
      m.t.Reset(flushLatency)
   }
   m.flushPending = true
   return
}

func (m *FlushWriter) delayedFlush() {
   m.mu.Lock()
   defer m.mu.Unlock()
   if !m.flushPending { // if stop was called but AfterFunc already started this goroutine
      return
   }
   m.dst.Flush()
   m.flushPending = false
}

func (m *FlushWriter) FlushIfNeeded() error {
   m.mu.Lock()
   defer m.mu.Unlock()
   var err error
   if m.flushPending {
      err = m.dst.Flush()
   }
   m.flushPending = false
   if m.t != nil {
      m.t.Stop()
   }
   return err
}

func (m *FlushWriter) Flush() error {
   m.mu.Lock()
   defer m.mu.Unlock()
   err := m.dst.Flush()
   m.flushPending = false
   if m.t != nil {
      m.t.Stop()
   }
   return err
}

func NewFlushWriter(w io.Writer) *FlushWriter {
   return &FlushWriter{dst: bufio.NewWriterSize(w, bufSize)}
}
