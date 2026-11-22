//go:build profile

package main

import (
   "os"
   "errors"
   "runtime/pprof"
   "time"
)

const doProf = true

const profSeconds = 10

func runProf() (err error) {
   if err = run(); err != nil {
      return
   }
   
   f, err := os.Create("cpu.prof")
   if err != nil {
      return
   }
   defer func() {
      err = errors.Join(err, f.Close())
   }()

   if err = pprof.StartCPUProfile(f); err != nil {
      return
   }
   defer pprof.StopCPUProfile()
   
   t := time.Now()
   for time.Since(t) < time.Second * profSeconds {
      if err = run(); err != nil {
         return
      }
   }
   
   return nil
}