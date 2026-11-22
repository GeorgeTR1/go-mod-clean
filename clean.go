package main

import (
   "os"
   "errors"
   "path/filepath"
   "os/exec"
   "bytes"
   "strings"
   "encoding/json"
// "slices"
   "math"
   "strconv"
   "io"
   "unicode"
   "unicode/utf8"
)

var errLogged = errors.New("logged")

func main() {
   exitVal := 0
   
   var err error
   if doProf {
      err = runProf()
   } else {
      err = run()
   }
   
   if err != nil {
      if err != errLogged {
         reportErr(err)
      }
      exitVal = 1
   }
   
   // will return an error if there was ever an error writing to stdout
   // see the documentation for bufio.Writer
   if err := StdoutWriter.Flush(); err != nil {
      exitVal += 2
   }
   if err := StderrWriter.Flush(); err != nil {
      exitVal += 4
   }
   
   os.Exit(exitVal)
}

func printHelp() {
   // using conventions from http://docopt.org/
   StderrWriteString(`Usage: go-mod-clean [-y] <path>...

  -y      Ignore prompts and always proceed with cleaning
  <path>  Either a directory that will be searched for go.mod files recursively, 
          or a path to a go.mod file
          
go-mod-clean is used to clean the go modcache of unused modules. You specify where
to search for go.mod files, and then those files are used to determine which modules
are still being used. Any modules still in the modcache that aren't being used will
then be deleted.

go-mod-clean version 0.1.2
`)
}

// used to report unknown errors that we don't handle specifically
func reportErr(err error) {
   StderrWriteString("ERROR: " + err.Error() + "\n")
}

// use a map with an empty struct to implement a basic set. See https://emersion.fr/blog/2017/sets-in-go/
var protected map[string]struct{}
var toolchains map[string]struct{}

var modcache string
var downloadCache string
var doToolchainCheck = false
var toolchainCache string
var toolchainPrefixLen int
var maxVersion string
var outBuf *bytes.Buffer
var errBuf *bytes.Buffer
var dec *json.Decoder

const matchAll = string(filepath.Separator) + "**"

func run() error {
   yes := false
   pathStart := 1
   
   if len(os.Args) > 1 && os.Args[1] == "-y" {
      yes = true
      pathStart = 2
   }
   
   if len(os.Args) <= pathStart {
      StderrWriteString("At least one path must be specified.\n\n")
      printHelp()
      return errLogged
   }
   
   isDirs := make([]bool, len(os.Args) - pathStart)
   
   // validate input
   for i, path := range os.Args[pathStart:] {
      inf, err := os.Stat(path)
      if errors.Is(err, os.ErrNotExist) {
         StderrWriteString("Path \"" + path + "\" does not exist.\n\n")
         printHelp()
         return errLogged
      }
      if err != nil {
         // unhandled stat error
         return err
      }
      
      if !inf.IsDir() && inf.Name() != "go.mod" {
         StderrWriteString("Path \"" + path + "\" is neither a directory nor a go.mod file.\n\n")
         printHelp()
         return errLogged
      }
      isDirs[i] = inf.IsDir()
   }
   
   StdoutWriteString("\nDetermining used files to protect. . .\n\n")
   
   foundGoMod := false
   protected = make(map[string]struct{})
   outBuf = &bytes.Buffer{}
   errBuf = &bytes.Buffer{}
   dec = json.NewDecoder(outBuf)
   
   // see https://go.dev/ref/mod#environment-variables
   if out := os.Getenv("GOMODCACHE"); out != "" {
      modcache = out
   } else if out := os.Getenv("GOPATH"); out != "" {
      modcache = filepath.Join(out, "pkg/mod")
   } else {
      out, err := os.UserHomeDir()
      if err != nil {
         return err
      }
      modcache = filepath.Join(out, "go/pkg/mod")
   }
   // see https://go.dev/ref/mod#module-cache
   // and https://cs.opensource.google/go/go/+/refs/tags/go1.26.4:src/cmd/go/internal/modfetch/cache.go;l=43
   downloadCache = filepath.Join(modcache, "cache/download")
   
   protected[modcache] = struct{}{}
   
   /* If a go module requires a version of go newer than the version installed on the
      system, the go tooling downloads the toolchain for the newer version and stores it
      in the modcache, and then uses that to compile the go module. If any of these
      toolchains have been installed, we run extra checks for each module to see if they
      are using a newer toolchain. If no toolchains are in the modcache, we can skip this
      step */
   _, err := os.Stat(downloadCache + "/golang.org/toolchain")
   if err == nil {
      doToolchainCheck = true
      toolchainCache = filepath.Join(downloadCache, "golang.org/toolchain/@v")
      toolchainPrefixLen = len(filepath.Join(modcache, "golang.org/toolchain@"))
      toolchains = make(map[string]struct{})
   } else if !errors.Is(err, os.ErrNotExist) {
      // unknown error from Stat
      return err
   }
   
   for i, path := range os.Args[pathStart:] {
      if isDirs[i] {
         err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
            if err != nil {
               goto reportErr
            }
            
            // use heuristics to determine if a directory is a vendor directory
            if d.IsDir() && d.Name() == "vendor" {
               _, err = os.Stat(path + "/modules.txt")
               if errors.Is(err, os.ErrNotExist) {
                  goto endVendorCheck
               }
               if err != nil {
                  goto reportErr
               }
               
               _, err = os.Stat(filepath.Dir(path) + "/go.mod")
               if errors.Is(err, os.ErrNotExist) {
                  goto endVendorCheck
               }
               if err != nil {
                  goto reportErr
               }
               
               abs, err := filepath.Abs(path)
               if err != nil {
                  goto reportErr
               }
               
               StdoutWriteString("Path \"" + abs + 
                  "\"\nnot checked for go.mod files, as it is assumed to be a vendor directory\n\n")
               return filepath.SkipDir
            }
            endVendorCheck:
            
            if d.Name() == "go.mod" {
               foundGoMod = true
               // handleGoMod handles its own errors and only returns
               // an error if the whole program should terminate
               err = handleGoMod(path)
               if err != nil {
                  return err
               }
            }
            
            return nil
            
            reportErr:
            // report error, but continue walking the directory
            reportErr(err)
            return nil
         })
         // indicates handleGoMod returned an error
         if err != nil {
            return err
         }
      } else {
         foundGoMod = true
         if err := handleGoMod(path); err != nil {
            return err
         }
      }
   }
   
   if !foundGoMod {
      StderrWriteString("No go.mod files found in paths\n\n")
      printHelp()
      return errLogged
   }
   
   /*
   StdoutWriteString("Protected paths:\n")
   paths := make([]string, 0, len(protected))
   for path := range protected {
      paths = append(paths, path)
   }
   slices.Sort(paths)
   for _, path := range paths {
      StdoutWriteString(path + "\n")
   }
   StdoutWriteString("\n")
   */
   
   StdoutWriteString("Paths to delete:\n")
   
   var deletePaths []string
   var deleteSize int64 = 0
   
   if len(protected) == 1 {
      // special case for deleting all of the modcache
      deleteSize = getDirSize(modcache)
      StdoutWriteString(modcache + string(filepath.Separator) + "\n")
   } else {
      err := filepath.WalkDir(modcache, func(path string, d os.DirEntry, err error) error {
         if err != nil {
            // report error, but continue walking the directory
            reportErr(err)
            return nil
         }
         
         if _, ok := protected[path]; ok {
            // path is protected, continue walking the tree
            return nil
         }
         // path is not protected
         
         if d.IsDir() {
            if _, ok := protected[path + matchAll]; !ok {
               // entire directory not protected
               path += string(filepath.Separator)
               StdoutWriteString(path + "\n")
               deletePaths = append(deletePaths, path)
               deleteSize += getDirSize(path)
            }
            // either the entire directory is protected, or the entire directory is not protected.
            // skip in either case
            return filepath.SkipDir
         }
         
         // file not protected
         StdoutWriteString(path + "\n")
         deletePaths = append(deletePaths, path)
         inf, err := d.Info()
         if err != nil {
            // report error, but continue walking the directory
            reportErr(err)
            return nil
         }
         deleteSize += inf.Size()
         
         return nil
      })
      if err != nil {
         // I'm pretty sure this should be unreachable based on how WalkDir works,
         // but I'll have it just in case
         reportErr(err)
      }
   }
   
   if deleteSize < 0 {
      panic("negative delete size")
   }
   StdoutWriteString("\nTotal Amount to Delete: " + IBytes(uint64(deleteSize)) + "\n\n")
   
   if len(toolchains) > 0 {
      var chainSize int64 = 0
      for chain := range toolchains {
         chainSize += getDirSize(chain)
      }
      if chainSize < 0 {
         panic("negative toolchain size")
      }
      StdoutWriteString(
`-------------------------------- NOTE -------------------------------------
At least one Go module requires a version of Go newer than the version
currently installed on the system. This causes Go to download a separate 
toolchain for each newer version required. This currently takes up ` + IBytes(uint64(chainSize)) + `.

To avoid this:
 - Update the version of Go installed on your system to at least ` + maxVersion + `
 - Run this tool again to delete the now unnecessary toolchain(s)
---------------------------------------------------------------------------

`)
   }
   
   if doProf {
      return nil
   }
   
   if !yes {
      inputBuf := make([]byte, 50)
      done := false
      quit := false
      for !done {
         StdoutWriteString("Permanently delete the files and folders listed above (y/n)? ")
         
         n, err := os.Stdin.Read(inputBuf)
         if err != nil && !errors.Is(err, io.EOF) {
            return err
         }
         
         trimmed := inputBuf[:n]
         trimmed = TrimSpaceLeft(trimmed)
         if len(trimmed) > 0 {
            switch trimmed[0] {
            case 'y', 'Y':
               done = true
            case 'n', 'N':
               done = true
               quit = true
            }
         }
         
         // consume all input from stdin
         for n == len(inputBuf) && inputBuf[n - 1] != '\n' {
            n, err = os.Stdin.Read(inputBuf)
            if err != nil && !errors.Is(err, io.EOF) {
               return err
            }
         }
      }
      if quit {
         return nil
      }
   }
   
   StdoutWriteString("Deleting...\n")
   
   if len(protected) == 1 {
      // special case for deleting all of the modcache
      runGoCmd("", "go clean -modcache", "clean", "-modcache")
   } else {
      for _, path := range deletePaths {
         if path[len(path) - 1] == filepath.Separator {
            // deleting a directory
            err := os.RemoveAll(path)
            if errors.Is(err, os.ErrPermission) {
               // the directory is write protected. We walk the directory recursively
               // enabling write permission
               err = filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
                  if err != nil {
                     // report error, but continue walking the directory
                     reportErr(err)
                     return nil
                  }
                  
                  if d.IsDir() {
                     err = os.Chmod(path, 0o755)
                  } else {
                     err = os.Chmod(path, 0o644)
                  }
                  
                  if err != nil {
                     // report error, but continue walking the directory
                     reportErr(err)
                  }
                  return nil
               })
               if err != nil {
                  // I'm pretty sure this should be unreachable based on how WalkDir works,
                  // but I'll have it just in case
                  reportErr(err)
               }
               err = os.RemoveAll(path)
               if err != nil {
                  reportErr(err)
               }
            } else if err != nil {
               // any other error from RemoveAll (the first time)
               reportErr(err)
            }
         } else {
            // deleting a file
            err := os.Remove(path)
            if errors.Is(err, os.ErrPermission) {
               // the file is write protected. Enable write permission
               err = os.Chmod(path, 0o644)
               if err != nil {
                  reportErr(err)
               }
               err = os.Remove(path)
               if err != nil {
                  reportErr(err)
               }
            } else if err != nil {
               // any other error from Remove (the first time)
               reportErr(err)
            }
         }
      }
   }
   
   StdoutWriteString("Done!\n")
   
   return nil
}

// this function only returns an error if the error means the entire program
// should terminate
func handleGoMod(path string) error {
   parent := filepath.Dir(path)
   
   // we run this first, as the toolchain check needs to be done even on
   // vendored modules
   if doToolchainCheck {
      err := checkToolchain(parent)
      if err != nil {
         return err
      }
   }
   
   _, err := os.Stat(parent + "/vendor/modules.txt")
   if err == nil {
      abs, err := filepath.Abs(path)
      if err != nil {
         reportErr(err)
         abs = path
      }
      StdoutWriteString("Dependencies for \"" + abs + 
         "\"\nwill not be processed, as it is assumed that this is a vendored module\n\n")
      return nil
   }
   if !errors.Is(err, os.ErrNotExist) {
      // unknown error from Stat
      reportErr(err)
      // don't proceed anyway, since we don't know if this is vendored or not
      return nil
   }
   
   /* the algorithm the go toolchain uses to determine which modules are imported is called
      Minimal version selection (MVS) (see https://go.dev/ref/mod#minimal-version-selection)
      It's quite complicated, and it seems like a much better idea to rely on the 'go' command
      as the source of truth when possible then try to re-implement these things ourselves */
   err = runGoCmd(parent, "go list -m all", "list", "-json=Path,Version,GoMod,Dir", "-m", "all")
   if err != nil {
      return err
   }
   
   type ModEntry struct {
      Path, 
      Version,
      GoMod,
      Dir string
   }
   
   for dec.More() {
      var m ModEntry
      err = dec.Decode(&m)
      if err != nil {
         // once a decoder has returned an error it won't decode again,
         // so we make a new one
         dec = json.NewDecoder(outBuf)
         reportErr(err)
         break
      }
      
      if len(m.GoMod) == 0 {
         /* only use this more involved method for getting paths when we have to,
            otherwise we use the GoMod path
         
            see https://go.dev/ref/mod#module-cache
            https://cs.opensource.google/go/go/+/refs/tags/go1.26.4:src/cmd/go/internal/modfetch/cache.go;l=43
            https://cs.opensource.google/go/go/+/refs/tags/go1.26.4:src/cmd/go/internal/modfetch/cache.go;l=64 */
         path := filepath.Join(downloadCache, escapeString(m.Path), "@v", escapeString(m.Version) + ".info")
         protected[path] = struct{}{}
         protectTree(path)
      } else {
         // skip files not in the modcache
         if !strings.HasPrefix(m.GoMod, modcache) {
            continue
         }
         
         // see https://go.dev/ref/mod#module-cache
         // 3 == len("mod")
         path := m.GoMod[:len(m.GoMod) - 3]
         protected[path + "mod"] = struct{}{}
         protected[path + "ziphash"] = struct{}{}
         protected[path + "info"] = struct{}{}
         protectTree(path)
      }
      
      if len(m.Dir) > 0 {
         protected[m.Dir + matchAll] = struct{}{}
         protectTree(m.Dir)
      }
   }
   
   return nil
}

// this function only returns an error if the error means the entire program
// should terminate
func checkToolchain(dir string) error {
   err := runGoCmd(dir, "go env", "env", "-json", "GOROOT", "GOVERSION")
   if err != nil {
      return err
   }
   
   type Env struct {
      GOROOT,
      GOVERSION string
   }
   var env Env
   err = json.Unmarshal(outBuf.Bytes(), &env)
   if err != nil {
      reportErr(err)
   }
   
   if strings.HasPrefix(env.GOROOT, modcache) {
      protected[env.GOROOT + matchAll] = struct{}{}
      protectTree(env.GOROOT)
      
      path := filepath.Join(toolchainCache, env.GOROOT[toolchainPrefixLen:] + ".ziphash")
      protected[path] = struct{}{}
      protectTree(path)
      
      if len(maxVersion) == 0 || env.GOVERSION > maxVersion {
         maxVersion = env.GOVERSION
      }
      
      toolchains[env.GOROOT] = struct{}{}
   }
   
   return nil
}

// this function only returns an error if the error means the entire program
// should terminate
func runGoCmd(dir, errMsg string, arg ...string) error {
   outBuf.Reset()
   errBuf.Reset()
   
   cmd := exec.Command("go", arg...)
   cmd.Dir = dir
   cmd.Stdout = outBuf
   cmd.Stderr = errBuf
   
   err := cmd.Run()
   
   if err != nil {
      if errors.Is(err, exec.ErrNotFound) {
         return errors.New("'go' command not found")
      }
      if _, ok := errors.AsType[*exec.ExitError](err); !ok {
         // unknown error running command
         reportErr(err)
      }
   }
   
   if errBuf.Len() > 0 {
      if len(dir) > 0 {
         abs, err := filepath.Abs(dir)
         if err != nil {
            reportErr(err)
            abs = dir
         }
         StderrWriteString("Error running '" + errMsg + "' in \"" + abs + "\":\n")
      } else {
         StderrWriteString("Error running '" + errMsg + "':\n")
      }
      StderrWrite(errBuf.Bytes())
      StderrWriteString("\n")
   }
   
   return nil
}

func protectTree(path string) {
   for {
      path = filepath.Dir(path)
      if _, ok := protected[path]; ok {
         break
      }
      protected[path] = struct{}{}
   }
}

/* Based on https://cs.opensource.google/go/x/mod/+/refs/tags/v0.38.0:module/module.go;l=707
   This is the exact function the go toolchain uses to determine the paths for modules.
   The error handling was removed, since these names have already been validated before
   being returned by 'go list'
   See also https://go.dev/ref/mod#module-cache and https://go.dev/ref/mod#go-mod-file-ident
   
   At https://pkg.go.dev/golang.org/x/mod/module#hdr-Unicode_Restrictions,
   it says "Although paths are currently disallowed from using Unicode, 
   we would like at some point to allow Unicode letters as well.." However, this is how things
   have been since before the release of Go 1.11 in 2018, which introduced modules, and the method hasn't
   changed since then. So I'm fine hard-coding this method rather than including golang.org/x/mod/module
   
   See
   https://cs.opensource.google/go/go/+/203c16592bd20ec22ed407d445719b90585626b1
   https://cs.opensource.google/go/go/+/203c16592bd20ec22ed407d445719b90585626b1:src/cmd/go/internal/module/module.go;l=355
   
   Also, the limits of only ASCII characters and this ! type encoding for capitals is officially documented
   at https://go.dev/ref/mod. While the functionality of the go tools is not covered by the Go compatibility promise
   (see https://go.dev/doc/go1compat#tools), if the modules system changes that is likely to break other functionality
   of this program as well
*/
func escapeString(s string) string {
   haveUpper := false
   for _, r := range s {
      if 'A' <= r && r <= 'Z' {
         haveUpper = true
         break
      }
   }

   if !haveUpper {
      return s
   }

   var buf []byte
   for _, r := range s {
      if 'A' <= r && r <= 'Z' {
         buf = append(buf, '!', byte(r + 'a' - 'A'))
      } else {
         buf = append(buf, byte(r))
      }
   }
   return string(buf)
}

func getDirSize(path string) int64 {
   var size int64 = 0
   err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
      if err != nil {
         // report error, but continue walking the directory
         reportErr(err)
         return nil
      }
      
      if !d.IsDir() {
         inf, err := d.Info()
         if err != nil {
            // report error, but continue walking the directory
            reportErr(err)
            return nil
         }
         size += inf.Size()
      }
      
      return nil
   })
   if err != nil {
      // I'm pretty sure this should be unreachable based on how WalkDir works,
      // but I'll have it just in case
      reportErr(err)
   }
   return size
}

// around 5x faster than the equivalent function in https://github.com/dustin/go-humanize/
func IBytes(s uint64) string {
   if s < 1024 {
      return strconv.Itoa(int(s)) + " B"
   }
   
   sizes := []string{" B", " KiB", " MiB", " GiB", " TiB", " PiB", " EiB"}
   const mask = 0x807f_ffff
   const shift = 23
   
   // using 32 bit floats is no faster or slower than 64 bit floats on my machine, 
   // but would probably be faster on a 32 bit system
   fBits := math.Float32bits(float32(s))
   // sign bit will always be 0
   exp := fBits >> shift
   unit := (exp - 127)/10
   f := math.Float32frombits((fBits & mask) | ((exp - unit*10) << shift))
   
   if f < 10 {
      return strconv.FormatFloat(float64(f), 'f', 2, 32) + sizes[unit]
   }
   if f < 100 {
      return strconv.FormatFloat(float64(f), 'f', 1, 32) + sizes[unit]
   }
   return strconv.Itoa(int(f + 0.5)) + sizes[unit]
}

// based on Go's implementation of bytes.TrimSpace
// https://cs.opensource.google/go/go/+/refs/tags/go1.26.4:src/bytes/bytes.go;l=1104

var asciiSpace = [256]uint8{'\t': 1, '\n': 1, '\v': 1, '\f': 1, '\r': 1, ' ': 1}

// TrimSpaceLeft returns a subslice of s by slicing off all leading
// white space, as defined by Unicode.
func TrimSpaceLeft(s []byte) []byte {
   // Fast path for ASCII: look for the first ASCII non-space byte.
   for lo, c := range s {
      if c >= utf8.RuneSelf {
         // If we run into a non-ASCII byte, fall back to the
         // slower Unicode-aware method on the remaining bytes.
         return bytes.TrimLeftFunc(s[lo:], unicode.IsSpace)
      }
      if asciiSpace[c] == 0 {
         return s[lo:]
      }
   }
   // Special case to preserve previous TrimLeftFunc behavior,
   // returning nil instead of empty slice if all spaces.
   return nil
}