package main

import (
   "os"
)

var StdoutWriter = NewFlushWriter(os.Stdout)
var StderrWriter = NewFlushWriter(os.Stderr)

/* Keep track of which output stream was last used last
   Allows us to flush when the output streams switch to keep the output order
   as expected */
var lastOutWasErr bool

/* These methods are not intended to be thread-safe
   We also aren't handling errors. Seems kind of unnecessary for standard outputs,
   especially when this is the only way we log errors */

func StdoutWrite(p []byte) int {
   if lastOutWasErr {
      StderrWriter.FlushIfNeeded()
   }
   lastOutWasErr = false
   
   n, _ := StdoutWriter.Write(p)
   return n
}

func StdoutWriteString(s string) int {
   if lastOutWasErr {
      StderrWriter.FlushIfNeeded()
   }
   lastOutWasErr = false
   
   n, _ := StdoutWriter.WriteString(s)
   return n
}

func StderrWrite(p []byte) int {
   if !lastOutWasErr {
      StdoutWriter.FlushIfNeeded()
   }
   lastOutWasErr = true
   
   n, _ := StderrWriter.Write(p)
   return n
}

func StderrWriteString(s string) int {
   if !lastOutWasErr {
      StdoutWriter.FlushIfNeeded()
   }
   lastOutWasErr = true
   
   n, _ := StderrWriter.WriteString(s)
   return n
}
