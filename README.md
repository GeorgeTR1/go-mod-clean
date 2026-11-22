`go-mod-clean` is a CLI tool for cleaning up unused files in the Go module cache.

## Install
```sh
go install -ldflags -s github.com/GeorgeTR1/go-mod-clean@latest
```

## Usage
```
Usage: go-mod-clean [-y] <path>...

  -y      Ignore prompts and always proceed with cleaning
  <path>  Either a directory that will be searched for go.mod files recursively, 
          or a path to a go.mod file
```          
`go-mod-clean` is used to clean the Go modcache of unused modules. You specify where to search for `go.mod` files, and then those files are used to determine which modules are still being used. Any modules still in the modcache that aren't being used will then be deleted.

This usage message will print when `go-mod-clean` is called with no arguments, as well as in other cases where the command is used improperly.

## Disclaimer

Although I have tested that this program works to the best of my ability on both Windows and Linux, I wouldn't consider myself an expert on the Go module system, so there may be bugs. However, if there are, the worst that might mean is that the program deletes files that actually were required for something. If that happens, though, those files will just be re-downloaded.

But while the project is in this state, if you're away from an internet connection and would be in a bad situation if files you relied on were deleted, maybe hold off on using this just yet.

If you do find any bugs, please let me know!

## Background

Any time the Go toolchain needs to download files for modules that are required for a given task, those modules are stored in the module cache, which is located at `~/go/pkg/mod` by default. This can happen when you call `go get`, `go build`, `go install`, or various other commands. However, unlike many other package managers, files are never removed from this directory, and it will only increase in size over time. The only method the go toolchain provides to clean this cache is `go clean -modcache`, but that deletes all contents of the directory. That includes modules that are still being used, so subsequent calls to `go build`, etc. will require re-downloading all the necessary module dependencies. If you have a slower internet connection, this could be a major headache.

`go-mod-clean` allows you to clean the modcache of any files that are no longer needed, while keeping those that you do still need. After running `go-mod-clean`, you can continue to build projects whose modules you preserved even without an internet connection, while still freeing up significant amounts of disk space.

I did find a [previous project](https://github.com/fosmjo/go-mod-clean) that aimed to do the same thing, but I had issues with it, namely:
 - It hasn't been update in a long time, despite having bugs.
 - I couldn't get it to work at all, probably because I tried it on Windows.
 - It has a few external dependencies, whereas this project uses only the Go standard library. It seemed counterproductive for a tool aimed at reducing the size of the modcache to actually increase the size of the modcache, and I didn't see a good reason why external libraries are necessary for a simple CLI tool like this.
 - It requires administrative privileges to delete files, whereas this project does not.
 - I thought the user interface was unnecessarily complex
 - I just wasn't a fan of how the code was structured

So, rather than forking that project, I decided to make a new one from scratch

## Building

If you just want to use the tool, run the [install](#install) command above. However, to build the project for development, clone the repo or otherwise download the source code and run
```
go run gnob.go
```
Then follow the instructions.

For further options you can pass to the `gnob` command, see the comments near the start of the `gnob.go` file

----

*No generative AI was used in any part of the creation of this software.*
