//go:build ignore

package main

import (
   "os"
   "os/exec"
   "path/filepath"
   "errors"
   "runtime"
   "io/fs"
   "time"
)

/* File 'gnob.go'
*  Inspired by https://github.com/tsoding/nob.h
*  Allows you to write a build system in go (presumably for go projects, but not necessarily)
*  Run 'go run gnob.go' to bootstrap, then just run ./gnob (or gnob on Windows) and gnob will detect
*  if it needs to rebuild itself, rebuild itself if necessary, and then run.
*
*  The steps of your build go in the 'recipe' function
*
*  Maybe a bit unnecessary when 'go run' exists, but it is faster when the file hasn't changed,
*  and typing ./gnob once gnob is bootstrapped is faster than go run build.go, or whatever
*  Was an interesting exercise regardless.
*
*  Also having it all be one file that's typically ignored by go build might not be the best
*  instead of having it as a separate package, or multiple packages to keep the recipe and
*  self-building code separate, but this is simple and works for my use
*/

// imports specific to this build recipe
import (
   "strconv"
)

/*
Usage: gnob [prof] [(run [<arg>]...)]

prof - build for profiling performance
run - run the program after compiling
arg - arguments to pass to the program when it runs

Usage syntax from http://docopt.org/
*/

func recipe() (err error) {
   buildDone := false
   
   // say "build failed" if the failure was during the building portion (i.e. before running)
   defer func() {
      if !buildDone {
         err = errors.Join(err, errors.New("\nBuild failed"))
      }
   }()
   
   const binName = "go-mod-clean"
   
   parmNext := 2
   prof := false
   if len(os.Args) >= parmNext && os.Args[parmNext - 1] == "prof" {
      parmNext++
      prof = true
   }
   
   // build routine
   if prof {
      err = cmd("go", "install", "-tags", "profile")
   } else {
      err = cmd("go", "install", "-ldflags", "-s")
   }
   if err != nil {
      return
   }
   
   buildDone = true
   
   var gobin []byte
   if out := os.Getenv("GOBIN"); out != "" {
      gobin = []byte(out)
   } else if out := os.Getenv("GOPATH"); out != "" {
      gobin = []byte(out + "/bin")
   } else {
      out, err = os.UserHomeDir()
      if err != nil {
         return
      }
      gobin = []byte(out + "/go/bin")
   }
   
   gobin = append(gobin, '/')
   gobin = append(gobin, binName...)
   if runtime.GOOS == "windows" {
      gobin = append(gobin, ".exe"...)
   }
   
   inf, err := os.Stat(string(gobin))
   if err != nil {
      return
   }
   
   os.Stdout.WriteString("\nExe size: " + strconv.FormatInt(inf.Size()/1000, 10) + " kB\n")
   
   profCmd := []string{"go", "tool", "pprof", "-http", ":8080", "cpu.prof"}
   
   if len(os.Args) >= parmNext && os.Args[parmNext - 1] == "run" {
      // run routine
      os.Stdout.WriteString("\n")
      err = cmd(binName, os.Args[parmNext:]...)
      if err != nil {
         return
      }
      if prof {
         os.Stdout.WriteString("\nRunning\n")
         os.Stdout.Write(concatCmd(profCmd))
         os.Stdout.WriteString("\n\n")
         err = cmd(profCmd[0], profCmd[1:]...)
         if err != nil {
            return
         }
      }
   } else if prof {
      os.Stdout.WriteString("\nRun\n")
      os.Stdout.Write(concatCmd(profCmd))
      os.Stdout.WriteString("\nto analyze profile\n")
   }
   
   return
}

func concatCmd(c []string) []byte {
   var b []byte
   for _, s := range c {
      b = append(b, s...)
      b = append(b, ' ')
   }
   return b
}

// =================================================================
// routines common to gnob (not specific to this build)
// Don't change anything past here unless you know what you're doing
// =================================================================

func main() {
   if err := run(); err != nil {
      os.Stderr.WriteString(err.Error() + "\n")
      os.Exit(1)
   }
}

var gnobBinName string

const gnobDelCommand = "_gnob_delete"

func run() (err error) {
   if runtime.GOOS == "windows" {
      gnobBinName = "gnob.exe"
   } else {
      gnobBinName = "gnob"
   }
   
   // gnob was called as a deleter. We always return from this, and never
   // do anything else if that was the case.
   if (len(os.Args) == 2 && os.Args[1] == gnobDelCommand) {
      for i := 0; i < 10; i++ {
         err = os.Remove(gnobBinName + ".old")
         if err == nil || errors.Is(err, fs.ErrNotExist) {
            // either success or file not found, we ignore the latter
            err = nil
            return
         }
         if !errors.Is(err, fs.ErrPermission) {
            // unknown error
            return
         }
         // must be permission error, we try again
         time.Sleep(100*time.Millisecond)
      }
      // we tried for a full second and still got a permission error, so return the error
      return
   }
   
   // see if we need to spawn a deleter no matter what, even if there was an error
   defer func() {
      err = errors.Join(err, spawnDeleter())
   }()
   
   inf, err := os.Stat("gnob.go")
   if errors.Is(err, fs.ErrNotExist) {
      err = errors.New("File 'gnob.go' not found.\n" +
      "Make sure you're running from the correct directory and haven't renamed the file.")
      return
   }
   if err != nil {
      return
   }
   
   _, srcFile, _, ok := runtime.Caller(0)
   if !ok || srcFile == "" {
      err = errors.New("runtime.Caller could not identify caller information")
      return
   }
   
   if !filepath.IsAbs(srcFile) {
      err = errors.New("runtime.Caller did not return an absolute path.\n" +
      "gnob should not be built with the -trimpath option")
      return
   }
   
   srcInf, err := os.Stat(srcFile)
   if err != nil && !errors.Is(err, fs.ErrNotExist) {
      err = errors.Join(errors.New("Unexpected error identifying source file of gnob:"), err)
      return
   }
   
   if err != nil || !os.SameFile(inf, srcInf) {
      var currAbs, srcAbs string
      currAbs, err = filepath.Abs("gnob.go")
      if err != nil {
         return
      }
      srcAbs, err = filepath.Abs(srcFile)
      if err != nil {
         return
      }
      
      const pathSep = string(os.PathSeparator)
      
      err = errors.New(
`It was detected that the 'gnob.go' file in the current working directory is not
the same as the source file used to build gnob. You must always run gnob or 
'go run gnob.go' in the same working directory as the gnob.go file of interest.

For example, running these commands is not allowed:
  otherDir` + pathSep + `gnob
  go run otherDir` + pathSep + `gnob.go


The two files of interest (these should be the same, but are not):

gnob.go in cwd: ` + currAbs + `
   source file: ` + srcAbs)
      return
   }
   
   srcTime := inf.ModTime()
   
   if srcTime.After(time.Now()) {
      /* The source file was last modified in the future
         If we don't do this, the program goes into an infinite loop continually
         rebuilding itself */
      err = os.Chtimes("gnob.go", time.Time{}, time.Now())
      if err != nil {
         return
      }
   }
   
   inf, err = os.Stat(gnobBinName)
   if errors.Is(err, fs.ErrNotExist) {
      // the executable does not exist, so gnob is being bootstrapped
      os.Stdout.WriteString("\nBootstrapping build system by building gnob . . .\n\n")
         
      err = buildYourself()
      if err != nil {
         return
      }
      
      os.Stdout.WriteString("Done\n\n" + 
      "Don't run 'go run gnob.go' anymore, it was only for bootstrapping.\n" +
      "Changes to gnob.go will cause gnob to rebuild itself before running.\n\n")
      
      if runtime.GOOS == "windows" {
         os.Stdout.WriteString("Run 'gnob' to build the project\n")
      } else {
         os.Stdout.WriteString("Run './gnob' to build the project\n")
      }
      return
   }
   
   // any other error from os.Stat
   if err != nil {
      return
   }
   
   binTime := inf.ModTime()
   
   if srcTime.After(binTime) {
      os.Stdout.WriteString("Detected change to gnob.go, rebuilding gnob . . .\n\n")
      
      /* on Windows you can't delete an executable out from under itself, but you can
         rename it. We then delete it as soon as we can by calling gnob with a command
         line argument that instructs it to delete the old executable */
      err = os.Rename(gnobBinName, gnobBinName + ".old")
      if err != nil {
         return
      }
      
      err = buildYourself()
      if err != nil {
         // rename back if the build failed, otherwise we can't run './gnob' anymore
         err = errors.Join(err, os.Rename(gnobBinName + ".old", gnobBinName))
         return
      }
      
      os.Stdout.WriteString("Done.\n\nRunning newly compiled gnob. . .\n")
      /* wait for child to complete before parent returns. The only reason for this is so
         you don't get a shell prompt stuck in between the parent and child. This does mean
         that the child can't delete the old executable of the parent, since it's still running,
         that's why we need a separate "deleter" process */
      err = cmd("./gnob", os.Args[1:]...)
      // if the child gnob had an error, it was already logged, so don't log it again
      if isExitErr(err) {
         err = nil
      }
      return
   }
   
   err = recipe()
   
   return
}

func buildYourself() error {
   // makes sure gnob is built for the current platform
   os.Setenv("GOOS", "")
   os.Setenv("GOARCH", "")
   
   // do not use trimpath or the necessary source file detection will probably break
   err := cmd("go", "build", "-ldflags", "-s", "gnob.go")
   if isExitErr(err) {
      return errors.New("\nBuild failed")
   }
   return err
}

func cmd(name string, arg ...string) error {
   cmd := exec.Command(name, arg...)
   cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr; cmd.Stdin = os.Stdin
   return cmd.Run()
}

func isExitErr(err error) bool {
   _, ok := errors.AsType[*exec.ExitError](err)
   return ok
}

/* If the old executable file exists, we call gnob again with a special command line arg
   so that it becomes a "deleter", whose only task is to attempt to delete the old
   executable. This time we don't wait for the child to return before the parent does, so
   the deleter can actually do its job. This shouldn't be an issue, since the deleter only
   writes to the console if something went very wrong. */
func spawnDeleter() error {
   _, err := os.Stat(gnobBinName + ".old")
   if errors.Is(err, fs.ErrNotExist) {
      return nil
   }
   if err != nil {
      return err
   }
   
   // old file exists
   cmd := exec.Command("./gnob", gnobDelCommand)
   cmd.Stderr = os.Stderr
   return cmd.Start()
}
